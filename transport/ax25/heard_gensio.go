// Copyright 2022 Corey Minyard (AE5KM). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package ax25

import (
	"time"
)

func GensioHeard() (map[string]time.Time) {
	heard := make(map[string]time.Time)
	gensioHeardMutex.Lock()
	for key, val := range gensioHeard {
		heard[key] = val
	}
	gensioHeardMutex.Unlock()
	return heard
}
