// Copyright 2022 Corey Minyard (AE5KM). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package ax25

import (
	"time"
	"net"
	"fmt"
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
		if a[0:5] == "addr:" {
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

func gLoop() {
	gax25Wait.Wait(1, nil)
}

func getBaseGensio(gensiostr, mycall string) (g gensio.Gensio, err error) {
	err = nil
	g = nil

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
		s := fmt.Sprintf("ax25(laddr=%s),%s", mycall, gensiostr)
		g = gensio.NewGensio(s, gax25o, &gevent{})
		g.OpenS()
		gax25str = gensiostr
		gax25call = mycall
		gax25 = g
		gax25_refcount = 1
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

func DialGensioAX25(gensiostr, mycall, targetcall string, timeout time.Duration) (rc net.Conn, err error) {
	g, err := getBaseGensio(gensiostr, mycall)
	if err != nil {
		return nil, err
	}
	defer func() {
		if r := recover(); r != nil {
			rc = nil
			err = fmt.Errorf("%s", r)
			putBaseGensio()
		}
	}()

	cg := &GConn{}
	args := []string{
		fmt.Sprintf("addr=0,%s,%s", targetcall, mycall),
	}
	c := g.AllocChannel(args, &gevent{})
	cg.gc, err = gensio.DialGensio(c)
	if err != nil {
		return nil, err
	}
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
