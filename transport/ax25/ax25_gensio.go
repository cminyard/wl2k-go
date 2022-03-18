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

type GensioLogHandler struct {
        gensio.LoggerBase
}

func (l *GensioLogHandler) Log(level int, log string) {
        fmt.Printf("GENSIO LOG(%s): %s\n", gensio.LogLevelToStr(level), log)
}

func DialGensioAX25(gensiostr, mycall, targetcall string, timeout time.Duration) (net.Conn, error) {
	s := fmt.Sprintf("ax25(laddr=%s,addr=\"0,%s,%s\"),%s",
		mycall, targetcall, mycall, gensiostr)
	return gensio.Dial(s, gensio.NewOsFuncs(0, &GensioLogHandler{}))
}
