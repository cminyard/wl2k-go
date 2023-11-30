// Copyright 2016 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package transport

import (
	"net/url"
	"path"
	"sort"
	"strings"
)

// URL contains all information needed to dial a remote node.
type URL struct {
	// TNC/modem/interface/network type.
	Scheme string

	// The host interface address.
	Host string

	// Host username (typically the local stations callsign) and password information.
	User *url.Userinfo

	// Target callsign.
	Target string

	// List of digipeaters ("path" between origin and target).
	Digis []string

	// List of query parameters.
	Params url.Values

	// Where script files are stored.
	Scripts string
}

// ParseURL parses a raw urlstring into an URL.
//
// scheme://(mycall(:password)@)(host)(/digi1/...)/targetcall
// Examples:
//   - ardop:///LA1B                        (Addresses LA1B on ARDOP).
//   - ax25://mycall@myaxport/LD5SK/LA1B-10 (Addresses LA1B-10 via LD5SK using AX.25-port "myaxport" and "MYCALL" as source callsign).
//
// The special query parameter host will override the host part of the path. (E.g. ax25:///LA1B?host=ax0 == ax25://ax0/LA1B).
func ParseURL(rawurl string) (*URL, error) {
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, err
	}

	// The digis and target should be all upper case
	u.Path = strings.ToUpper(u.Path)

	via, target := path.Split(u.Path)
	if len(target) < 3 {
		return nil, ErrInvalidTarget
	}

	url := &URL{
		Scheme: u.Scheme,
		Host:   u.Host,
		User:   u.User,
		Target: target,
		Params: u.Query(),
	}

	if str := url.Params.Get("host"); str != "" {
		url.Host = str
	}

	// Digis
	url.Digis = strings.Split(strings.Trim(via, "/"), "/")
	_ = sort.Reverse(sort.StringSlice(url.Digis))
	if len(url.Digis) == 1 && url.Digis[0] == "" {
		url.Digis = []string{}
	}

	// TODO: This should be up to the specific transport to decide.
	digisUnsupported := url.Scheme == "ardop" || url.Scheme == "telnet"
	if len(url.Digis) > 0 && digisUnsupported {
		return url, ErrDigisUnsupported
	}

	return url, nil
}

// Set the URL.User's username (usually the source callsign).
func (u *URL) SetUser(call string) { u.User = url.User(call) }
