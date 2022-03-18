// Copyright 2022 Corey Minyard (AE5KM). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package ax25

import (
	"time"
	"net"
	"fmt"
	"github.com/cminyard/go/gensio"
)

type gensioAX25Beacon struct {
	message string
	localAddr string
	remoteAddr string
	g gensio.Gensio
}

func NewGensioAX25Beacon(gensiostr, mycall, dest, message string) (rb Beacon, err error) {
	b := &gensioAX25Beacon{}
	b.localAddr = mycall
	b.remoteAddr = dest
	b.message = message
	b.g, err = getBaseGensio(gensiostr, mycall)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (b *gensioAX25Beacon) Message() string {
	return b.message
}

func (b *gensioAX25Beacon) LocalAddr() net.Addr {
	return gensio.NewAx25NetAddr(b.localAddr)
}

func (b *gensioAX25Beacon) RemoteAddr() net.Addr {
	return gensio.NewAx25NetAddr(b.remoteAddr)
}

func (b *gensioAX25Beacon) Every(d time.Duration) error {
	for {
		if err := b.Now(); err != nil {
			return err
		}
		time.Sleep(d)
	}
}

func (b *gensioAX25Beacon) Now() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s", r)
		}
	}()

	b.g.Write([]byte(b.message),
		[]string{"oob", fmt.Sprintf("addr:0,%s,%s", b.remoteAddr, b.localAddr)})

	return nil
}
