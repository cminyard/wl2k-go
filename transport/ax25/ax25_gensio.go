// Copyright 2022 Corey Minyard (AE5KM). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package ax25

import (
	"time"
	"net"
	"fmt"
	"context"
	"errors"
	"sync"
	"runtime"
	"strings"
	"github.com/cminyard/go/gensio"
)

type GensioLogHandler struct {
        gensio.LoggerBase
}

func (l *GensioLogHandler) Log(level int, log string) {
        fmt.Printf("GENSIO LOG(%s): %s\n", gensio.LogLevelToStr(level), log)
}

var gmutex sync.Mutex
var gax25_refcount uint = 0
var gax25_listeners uint = 0
var gax25str string
var gax25call string
var gax25 gensio.Gensio = nil
var gax25o *gensio.OsFuncs = gensio.NewOsFuncs(&GensioLogHandler{})
var gax25Wait *gensio.Waiter = gensio.NewWaiter(gax25o)
var gax25Accepts chan *GConn = make(chan *GConn, 10)

type gevent struct {
	gensio.EventBase
}

func (e *gevent) NewChannel(new_channel gensio.Gensio, auxdata []string) int {
	var addr string
	for _, a := range(auxdata) {
		if len(a) > 5 && a[0:5] == "addr:" {
			addr = a[5:]
			break;
		}
	}
	if len(addr) == 0 || gax25_listeners == 0 {
		return gensio.GE_NOTSUP
	} else {
		gc := &GConn{}
		gc.g = new_channel
		// Can't create the Conn from the gensio here, we are
		// in a callback and SetSync() requires all callbackes
		// to be complete before returning.
		ss := strings.Split(addr, ",")
		if len(ss) < 2 {
			return gensio.GE_NOTSUP
		}
		gc.remoteAddr = ss[1]
		gax25Accepts <- gc
		return 0
	}
}

var gensioHeardMutex sync.Mutex
var gensioHeard map[string]time.Time = make(map[string]time.Time)

// UI frames come in here
func (e *gevent) Read(err int, data []byte, auxdata []string) uint64 {
	oobfound := false
	srcaddrfound := false
	var srcaddr string
	for _, s := range(auxdata) {
		if s == "oob" {
			oobfound = true
		}
		if len(s) > 5 && s[0:5] == "addr:" {
			srcaddr = s[5:]
			srcaddrfound = true
		}
	}
	if (!oobfound || !srcaddrfound) {
		return uint64(len(data))
	}
	ss := strings.Split(srcaddr, ",")
	if (len(ss) < 3) {
		return uint64(len(data))
	}
	srcaddr = ss[2]

	gensioHeardMutex.Lock()
	gensioHeard[srcaddr] = time.Now()
	gensioHeardMutex.Unlock()
	return uint64(len(data))
}

func gLoop() {
	gax25Wait.Wait(1, nil)
}

func getBaseGensio(gensiostr, mycall string) (g gensio.Gensio, err error) {
	err = nil
	g = nil
	extraparms := ""

	if strings.HasPrefix(gensiostr, "(") {
		// Parameters for ax25
		end := strings.Index(gensiostr, ")")
		if end == -1 {
			return nil, fmt.Errorf("gensio string parameters invalid")
		}
		extraparms = gensiostr[0:end]
		extraparms = strings.Replace(extraparms, "(", ",", 1)
		gensiostr = gensiostr[end + 1:]
	}

	gmutex.Lock()
	if gax25 != nil {
		if gensiostr != gax25str {
			err = fmt.Errorf("Gensio string doesn't match")
		} else if mycall != gax25call {
			err = fmt.Errorf("Gensio call sign doesn't match")
		} else {
			g = gax25
			gax25_refcount++
		}
	} else {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("%s", r)
			}
		}()
		s := fmt.Sprintf("ax25(laddr=%s%s),%s", mycall, extraparms, gensiostr)
		g = gensio.NewGensio(s, gax25o, &gevent{})
		g.OpenS()
		gax25str = gensiostr
		gax25call = mycall
		gax25 = g
		gax25_refcount = 1
		g.SetReadCallbackEnable(true)
		g.Control(0, false, gensio.GENSIO_CONTROL_ENABLE_OOB,
			[]byte("2"))
		go gLoop()
	}
	gmutex.Unlock()

	return g, err
}

func putBaseGensio() {
	gmutex.Lock()
	if gax25_refcount == 0 {
		panic("gax25_refcount decremented when zero")
	}
	gax25_refcount--
	if gax25_refcount == 0 {
		gax25.CloseS()
		gax25 = nil
		gax25str = ""
		gax25call = ""
	}
	gmutex.Unlock()
}

type destroyer interface {
	destroy()
}

// We wrap our own Conn so we can catch the close an put the base gensio.
// Also so we can GC this.
type GConn struct {
	gc *gensio.Conn
	g gensio.Gensio
	closed bool
	localAddr string
	remoteAddr string
}

func (c *GConn) Read(b []byte) (n int, err error) {
	return c.gc.Read(b)
}

func (c *GConn) Write(b []byte) (n int, err error) {
	return c.gc.Write(b)
}

func (c *GConn) Close() (err error) {
	if c.closed {
		return errors.New("Connection is closed")
	}
	err = c.gc.Close()
	if err != nil {
		return err
	}
	c.closed = true
	putBaseGensio()
	return nil
}

func (c *GConn) LocalAddr() net.Addr {
	return gensio.NewAx25NetAddr(c.localAddr)
}

func (c *GConn) RemoteAddr() net.Addr {
	return gensio.NewAx25NetAddr(c.remoteAddr)
}

func (c *GConn) SetDeadline(t time.Time) error {
	return c.gc.SetDeadline(t)
}

func (c *GConn) SetReadDeadline(t time.Time) error {
	return c.gc.SetReadDeadline(t)
}

func (c *GConn) SetWriteDeadline(t time.Time) error {
	return c.gc.SetWriteDeadline(t)
}

func (c *GConn) destroy() {
	if !c.closed {
		c.Close()
	}
}

func doneAX25Gensio(g gensio.Gensio, ctx context.Context, closechan <- chan int) {
	select {
	case <- ctx.Done():
		g.CloseS()
	case <- closechan:
	}
}

func DialGensioAX25Context(ctx context.Context,
	gensiostr, mycall, targetcall, parms, script string) (rc net.Conn, err error) {
	// Split up the target call into individual calls
	addrs := strings.Split(targetcall, " ")
	targetcall = addrs[0]
	route := ""
	for i := 2; i < len(addrs); i++ {
		route = fmt.Sprintf("%s,%s", route, addrs[i])
	}

	g, err := getBaseGensio(gensiostr, mycall)
	if err != nil {
		return nil, err
	}
	closechan := make(chan int, 1)
	defer func() {
		if r := recover(); r != nil {
			rc = nil
			err = fmt.Errorf("%s", r)
			putBaseGensio()
		}
		closechan <- 1
	}()

	cg := &GConn{}
	sparms := []string{}
	if len(parms) > 0 {
		sparms = strings.Split(parms, ",")
	}
	args := make([]string, 1 + len(sparms))
	args[0] = fmt.Sprintf("addr=0,%s,%s%s", targetcall, mycall, route)
	for i, v := range(sparms) {
		args[i + 1] = v
	}

	c := g.AllocChannel(args, &gevent{})
	if len(script) > 0 {
		scrstr := fmt.Sprintf("script(script=%s)", script)
		sg := gensio.NewGensioChild(c, scrstr, gax25o, &gevent{})
		c = sg
	}
	cg.gc, err = gensio.DialGensio(c)
	if err != nil {
		return nil, err
	}
	go doneAX25Gensio(c, ctx, closechan)
	cg.remoteAddr = targetcall
	cg.localAddr = mycall
	c.OpenS()
	runtime.SetFinalizer(cg, destroyer.destroy)
	return cg, nil
}

type Listener struct {
	quit chan bool
	localAddr string
}

func (l *Listener) Accept() (rc net.Conn, err error) {
	defer func() {
		if r := recover(); r != nil {
			rc = nil
			err = fmt.Errorf("%s", r)
		}
	}()
	var c *GConn
	select {
	case c = <- gax25Accepts:

	case <- l.quit:
		return nil, fmt.Errorf("%s", "Listener was closed")
	}
	c.gc, err = gensio.DialGensio(c.g)
	if err != nil {
		c.g.CloseS()
		return nil, err
	}
	c.localAddr = l.localAddr
	gmutex.Lock()
	gax25_refcount++
	gmutex.Unlock()
	runtime.SetFinalizer(c, destroyer.destroy)
	return c, nil
}

func (l *Listener) Close() (err error) {
	gmutex.Lock()
	l.quit <- true
	gax25_listeners--
	if gax25_listeners == 0 {
		done := false
		for !done {
			select {
			case c := <- gax25Accepts:
				c.gc.Close()
			default:
				done = true
			}
		}
	}
	gmutex.Unlock()
	putBaseGensio()
	return nil
}

func (l *Listener) Addr() net.Addr {
	return gensio.NewAx25NetAddr(l.localAddr)
}

func (l *Listener) destroy() {
	l.Close()
}

func ListenGensioAX25(gensiostr, mycall string) (rc net.Listener, err error) {
	l := &Listener{}
	l.quit = make(chan bool)
	l.localAddr = mycall
	getBaseGensio(gensiostr, mycall)
	defer func() {
		if r := recover(); r != nil {
			rc = nil
			err = fmt.Errorf("%s", r)
			putBaseGensio()
		}
	}()
	gmutex.Lock()
	gax25_listeners++
	gmutex.Unlock()
	runtime.SetFinalizer(l, destroyer.destroy)

	return l, nil
}
