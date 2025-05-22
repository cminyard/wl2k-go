// Copyright 2015 Martin Hebnes Pedersen (LA5NTA). All rights reserved.
// Use of this source code is governed by the MIT-license that can be
// found in the LICENSE file.

package fbb

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
)

var ErrNoFB2 = errors.New("Remote does not support B2 Forwarding Protocol")

// IsLoginFailure returns a boolean indicating whether the error is known to
// report that the secure login failed.
func IsLoginFailure(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "secure login failed")
}

func (s *Session) handshake(rw io.ReadWriter) error {
	if s.master {
		// Send MOTD lines
		for _, line := range s.motd {
			fmt.Fprintf(rw, "%s\r", line)
		}

		if err := s.sendHandshake(rw, ""); err != nil {
			return err
		}
	}

	hs, err := s.readHandshake()
	if err != nil {
		return err
	}

	// Did we get SID codes?
	if hs.SID == "" {
		return errors.New("No sid in handshake")
	}

	s.remoteSID = hs.SID
	s.remoteFW = hs.FW

	if !s.master {
		return s.sendHandshake(rw, hs.SecureChallenge)
	} else {
		return nil
	}
}

type handshakeData struct {
	SID             sid
	FW              []Address
	SecureChallenge string
}

func (s *Session) readHandshake() (handshakeData, error) {
	data := handshakeData{}

	for {
		if bytes, err := s.rd.Peek(1); err != nil {
			return data, err
		} else if bytes[0] == 'F' && s.master {
			return data, nil // Next line is a protocol command, handshake is done
		}

		// Ignore remote errors here, as the server sometimes sends lines like
		// '*** MTD Stats Total connects = 2580 Total messages = 3900', which
		// are not errors
		line, err := s.nextLineRemoteErr(false)
		if err != nil {
			return data, err
		}

		//REVIEW: We should probably be more strict on what to allow here,
		// to ensure we disconnect early if the remote is not talking the expected
		// protocol. (We should at least allow unknown ; prefixed lines aka "comments")
		switch {
		// Header with sid (ie. [WL2K-2.8.4.8-B2FWIHJM$])
		case isSID(line):
			data.SID, err = parseSID(line)
			if err != nil {
				return data, err
			}

			// Do we support the remote's SID codes?
			if !data.SID.Has(sFBComp2) { // We require FBB compressed protocol v2 for now
				return data, ErrNoFB2
			}
		case strings.HasPrefix(line, ";FW"): // Forwarders
			data.FW, err = parseFW(line)
			if err != nil {
				return data, err
			}
		case strings.HasPrefix(line, ";PQ"): // Secure password challenge
			data.SecureChallenge = line[5:]

		case strings.HasSuffix(line, ">"): // Prompt
			return data, nil
		default:
			// Ignore
		}
	}
}

func (s *Session) sendHandshake(writer io.Writer, secureChallenge string) error {
	if secureChallenge != "" && s.secureLoginHandleFunc == nil {
		return errors.New("Got secure login challenge, please register a SecureLoginHandleFunc.")
	}

	w := bufio.NewWriter(writer)

	// Request messages on behalf of every localFW
	fmt.Fprintf(w, ";FW:")
	for i, addr := range s.localFW {
		switch {
		case secureChallenge != "" && i > 0:
			// Include passwordhash for auxiliary addresses (required by WL2K-4.x or later)
			if password, _ := s.secureLoginHandleFunc(addr); password != "" {
				resp := secureLoginResponse(secureChallenge, password)
				// In the B2F specs they use space as delimiter, but Winlink Express uses pipe.
				// I'm not sure space as a delimiter would even work when passwords for aux addresses
				// are optional (according to the very same document).
				fmt.Fprintf(w, " %s|%s", addr.Addr, resp)
				break
			}
			// Password is not required for all aux addresses according to Winlink's B2F specs.
			fallthrough
		default:
			fmt.Fprintf(w, " %s", addr.Addr)
		}
	}
	fmt.Fprintf(w, "\r")

	writeSID(w, s.ua.Name, s.ua.Version)

	if secureChallenge != "" {
		password, err := s.secureLoginHandleFunc(s.localFW[0])
		if err != nil {
			return err
		}
		resp := secureLoginResponse(secureChallenge, password)
		writeSecureLoginResponse(w, resp)
	}

	fmt.Fprintf(w, "; %s DE %s (%s)", s.targetcall, s.mycall, s.locator)
	if s.master {
		fmt.Fprintf(w, ">\r")
	} else {
		fmt.Fprintf(w, "\r")
	}

	return w.Flush()
}

func parsePM(str string) (PendingMessage, error) {
	str = strings.TrimPrefix(str, ";PM: ")

	// TO MID SIZE FROM SUBJECT
	parts := strings.SplitN(str, " ", 5)
	if len(parts) != 5 {
		return PendingMessage{}, fmt.Errorf("Unexpected number of fields (%d): %q", len(parts), str)
	}
	parseInt := func(str string) int { n, _ := strconv.Atoi(str); return n }
	return PendingMessage{
		To:      AddressFromString(parts[0]),
		MID:     parts[1],
		Size:    parseInt(parts[2]),
		From:    AddressFromString(parts[3]),
		Subject: parts[4],
	}, nil
}

func parseFW(line string) ([]Address, error) {
	if !strings.HasPrefix(line, ";FW: ") {
		return nil, errors.New("Malformed forward line")
	}

	fws := strings.Split(line[5:], " ")
	addrs := make([]Address, 0, len(fws))

	for _, str := range strings.Split(line[5:], " ") {
		str = strings.Split(str, "|")[0] // Strip password hashes (unsupported)
		addrs = append(addrs, AddressFromString(str))
	}

	return addrs, nil
}

type sid string

const localSID = sFBComp2 + sFBBasic + sHL + sMID + sBID

// The SID codes
const (
	sAckForPM   = "A"  // Acknowledge for person messages
	sFBBasic    = "F"  // FBB basic ascii protocol supported
	sFBComp0    = "B"  // FBB compressed protocol v0 supported
	sFBComp1    = "B1" // FBB compressed protocol v1 supported
	sFBComp2    = "B2" // FBB compressed protocol v2 (aka B2F) supported
	sHL         = "H"  // Hierarchical Location designators supported
	sMID        = "M"  // Message identifier supported
	sCompBatchF = "X"  // Compressed batch forwarding supported
	sI          = "I"  // "Identify"? Palink-unix sends ";target de mycall QTC n" when remote has this
	sBID        = "$"  // BID supported (must be last character in SID)

	sGzip = "G" // Gzip compressed messages supported (GZIP_EXPERIMENT)
)

func gzipExperimentEnabled() bool { return os.Getenv("GZIP_EXPERIMENT") == "1" }

func writeSID(w io.Writer, appName, appVersion string) error {
	sid := localSID

	if gzipExperimentEnabled() {
		sid = sid[0:len(sid)-1] + sGzip + sid[len(sid)-1:]
	}

	_, err := fmt.Fprintf(w, "[%s-%s-%s]\r", appName, appVersion, sid)
	return err
}

func writeSecureLoginResponse(w io.Writer, response string) error {
	_, err := fmt.Fprintf(w, ";PR: %s\r", response)
	return err
}

func isSID(str string) bool {
	return strings.HasPrefix(str, `[`) && strings.HasSuffix(str, `]`)
}

func parseSID(str string) (sid, error) {
	code := regexp.MustCompile(`\[.*-(.*)\]`).FindStringSubmatch(str)
	if len(code) != 2 {
		return sid(""), errors.New(`Bad SID line: ` + str)
	}

	return sid(
		strings.ToUpper(code[len(code)-1]),
	), nil
}

func (s sid) Has(code string) bool {
	return strings.Contains(string(s), strings.ToUpper(code))
}
