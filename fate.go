package fate

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/godcong/fate/config"
	"github.com/goextension/log"
	"github.com/xormsharp/xorm"
	"strings"
	"time"

	"github.com/godcong/chronos"
	"github.com/godcong/yi"
)

var DefaultDatabase = "fate.db"
var DefaultStrokeMax = 32
var DefaultStrokeMin = 0
var HardMode = false

type Fate interface {
	MakeName(ctx context.Context) (e error)
	XiYong() *XiYong
	//SetCharDB(engine *xorm.Engine)
	//GetLastCharacter() error
}

type fateImpl struct {
	config   config.Config
	db       Database
	born     chronos.Calendar
	last     []string
	lastChar []*Character
	names    []*Name
	nameType int
	sex      string

	debug   bool
	isFirst bool
	Limit   int
	baZi    *BaZi
	zodiac  *Zodiac
}

type Options func(f *fateImpl)

func DBOption(database Database) Options {
	return func(f *fateImpl) {
		f.db = database
	}
}

func ConfigOption(cfg config.Config) Options {
	return func(f *fateImpl) {
		f.config = cfg
	}
}

func Debug() Options {
	return func(f *fateImpl) {
		f.debug = true
	}
}

//NewFate 所有的入口,新建一个fate对象
func NewFate(lastName string, born time.Time, options ...Options) Fate {
	f := &fateImpl{
		last: strings.Split(lastName, ""),
		born: chronos.New(born),
	}
	f.lastChar = make([]*Character, len(f.last))
	if len(f.last) > 2 {
		panic("last name could not bigger than 2 characters")
	}

	for _, op := range options {
		op(f)
	}

	f.init()

	return f
}

func (f *fateImpl) RandomName() {
	//filterWuGe(f.db, f.last...)
}

func (f *fateImpl) getLastCharacter() error {
	size := len(f.last)
	if size == 0 {
		return errors.New("last name was not inputted")
	} else if size > 2 {
		return fmt.Errorf("%d characters last name was not supported", size)
	} else {
		//ok
	}

	for i, c := range f.last {
		character, e := f.db.GetCharacter(Char(c))
		if e != nil {
			return e
		}
		f.lastChar[i] = character
	}
	return nil
}

func (f *fateImpl) MakeName(ctx context.Context) (e error) {
	log.Info("正在使用Fate生成姓名列表，如遇到问题请访问项目地址：https://github.com/godcong/fate获取帮助：")
	n, e := f.db.CountWuGeLucky()
	if e != nil {
		return Wrap(e, "count total error")
	}
	f.isFirst = n == 0
	if f.isFirst {
		lucky := make(chan *WuGeLucky)
		go initWuGe(lucky)
		for la := range lucky {
			_, e = f.db.InsertOrUpdateWuGeLucky(la)
			if e != nil {
				return Wrap(e, "insert failed")
			}
		}
	}

	e = f.getLastCharacter()
	if e != nil {
		return Wrap(e, "get char failed")
	}
	name := make(chan *Name)
	go func() {
		e := f.getWugeName(name)
		if e != nil {
			log.Error(e)
		}
	}()

	var tmpChar []*Character
	//supplyFilter := false
	var w *bufio.Writer
	if f.config.FileOutput != "" {
		w, e = NewOutput(f.config.FileOutput)
		if e != nil {
			return e
		}
		_, e = w.WriteString("名字;本卦;变卦;拼音;八字;喜用神\n")
		if e != nil {
			return e
		}
	}

	for n := range name {
		select {
		case <-ctx.Done():
			log.Info("end")
			return
		default:

		}

		tmpChar = n.FirstName
		tmpChar = append(tmpChar, n.LastName...)
		//filter bazi
		if f.config.SupplyFilter && !filterXiYong(f.XiYong().Shen(), tmpChar...) {
			//log.Infow("supply", "name", n.String())
			continue
		}
		//filter zodiac
		if f.config.ZodiacFilter && !filterZodiac(f.born, n.FirstName...) {
			//log.Infow("zodiac", "name", n.String())
			continue
		}
		//filter bagua
		if f.config.BaguaFilter && !filterYao(n.BaGua(), "凶") {
			//log.Infow("bagua", "name", n.String())
			continue
		}
		ben := n.BaGua().Get(yi.BenGua)
		bian := n.BaGua().Get(yi.BianGua)
		if f.debug {
			log.Infow("bazi", "born", f.born.LunarDate(), "time", f.born.Lunar().EightCharacter())
			log.Infow("xiyong", "wuxing", n.WuXing(), "god", f.XiYong().Shen(), "pinheng", f.XiYong())
			log.Infow("ben", "ming", ben.GuaMing, "chu", ben.ChuYaoJiXiong, "er", ben.ErYaoJiXiong, "san", ben.SanYaoJiXiong, "si", ben.SiYaoJiXiong, "wu", ben.WuYaoJiXiong, "liu", ben.ShangYaoJiXiong)
			log.Infow("bian", "ming", bian.GuaMing, "chu", bian.ChuYaoJiXiong, "er", bian.ErYaoJiXiong, "san", bian.SanYaoJiXiong, "si", bian.SiYaoJiXiong, "wu", bian.WuYaoJiXiong, "liu", bian.ShangYaoJiXiong)
		}

		if w != nil {
			_, e := w.WriteString(strings.Join([]string{n.String(), ben.GuaMing, bian.GuaMing, n.PinYin(), strings.Join(f.born.Lunar().EightCharacter(), ""), f.XiYong().Shen()}, ",") + "\n")
			if e != nil {
				log.Error("output error", "error", e)
			}
		} else {
			log.Infow("Output-->", "名字", n.String(), "笔画", n.Strokes(), "拼音", n.PinYin(), "八字", f.born.Lunar().EightCharacter(), "喜用神", f.XiYong().Shen(), "本卦", ben.GuaMing, "变卦", bian.GuaMing)
		}
	}
	return nil
}

func (f *fateImpl) XiYong() *XiYong {
	if f.baZi == nil {
		f.baZi = NewBazi(f.born)
	}
	return f.baZi.XiYong()
}

func (f *fateImpl) init() {
	var e error
	if f.db == nil {
		panic("database was not set")
	}

	e = f.db.Sync(WuGeLucky{})
	if e != nil {
		panic(e)
	}
}

//SetBornData 设定生日
func (f *fateImpl) SetBornData(t time.Time) {
	f.born = chronos.New(t)
}

func (f *fateImpl) getWugeName(name chan<- *Name) (e error) {
	defer func() {
		close(name)
	}()
	lucky := make(chan *WuGeLucky)
	go func() {
		e = f.db.FilterWuGe(f.lastChar, lucky)
		if e != nil {
			log.Error(e)
			return
		}
	}()
	var f1s []*Character
	var f2s []*Character
	for l := range lucky {

		if f.config.HardMode && hardFilter(l) {
			sc := NewSanCai(l.TianGe, l.RenGe, l.DiGe)
			if !Check(f.db.Database().(*xorm.Engine), sc, 5) {
				continue
			}
		}

		if f.config.StrokeMin > l.FirstStroke1 || f.config.StrokeMin > l.FirstStroke2 || f.config.StrokeMax < l.FirstStroke1 || f.config.StrokeMax < l.FirstStroke2 {
			continue
		}

		if f.debug {
			log.Infow("lucky", "l1", l.LastStroke1, "l2", l.LastStroke2, "f1", l.FirstStroke1, "f2", l.FirstStroke2)
		}

		f1s, e = f.db.GetCharacters(Stoker(l.FirstStroke1))
		if e != nil {
			return Wrap(e, "first stroke1 error")
		}
		f2s, e = f.db.GetCharacters(Stoker(l.FirstStroke2))
		if e != nil {
			return Wrap(e, "first stoke2 error")
		}

		for _, f1 := range f1s {
			if len(f1.PinYin) == 0 {
				continue
			}
			for _, f2 := range f2s {
				if len(f2.PinYin) == 0 {
					continue
				}
				n := createName(f, f1, f2)
				name <- n
			}
		}
	}
	return nil
}

func isLucky(s string) bool {
	if strings.Compare(s, "吉") == 0 || strings.Compare(s, "半吉") == 0 {
		return true
	}
	return false
}

func hardFilter(lucky *WuGeLucky) bool {
	if !isLucky(GetDaYan(lucky.DiGe).Lucky) ||
		!isLucky(GetDaYan(lucky.RenGe).Lucky) ||
		!isLucky(GetDaYan(lucky.WaiGe).Lucky) ||
		!isLucky(GetDaYan(lucky.ZongGe).Lucky) {
		return true
	}
	return false
}
