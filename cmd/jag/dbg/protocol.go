// Copyright (C) 2026 Toitware ApS. All rights reserved.
// Use of this source code is governed by an MIT-style license that can be
// found in the LICENSE file.

// Package dbg is the transport-agnostic core of the jag host debugger: a pure
// dbg: line-protocol parser, offline method-name resolution, and a relay engine
// driven entirely through the Channel interface. It has no dependency on
// os/exec, pipes, or cobra so the device path is a new Channel impl, not a
// rewrite.
package dbg

import (
	"regexp"
	"strconv"
	"strings"
)

// Kind classifies a parsed line from the VM's combined stdout stream.
type Kind string

const (
	KindReady  Kind = "ready"
	KindPaused Kind = "paused"
	KindStack  Kind = "stack"
	KindOK     Kind = "ok"
	KindError  Kind = "error"
	KindApp    Kind = "app"   // the debugged program's own output
	KindOther  Kind = "other" // a dbg: line we do not recognize
)

// Event is the parsed form of one VM stdout line.
type Event struct {
	Kind Kind
	Mode string         // paused: "break" | "step"
	ID   int            // paused: method id (-1 = entry/no method)
	Off  int            // paused/stack: bytecode offset
	Regs map[int]string // stack: register index -> value
	Verb string         // ok: the acknowledged verb
	Msg  string         // error: the message
	Text string         // app/other: the raw line
}

var (
	pausedRe = regexp.MustCompile(`^dbg:paused (break|step) (-?\d+) (\d+)$`)
	offRe    = regexp.MustCompile(`off=(\d+)`)
	regRe    = regexp.MustCompile(`r(\d+)=(\S+)`)
)

// ParseLine parses one line of VM stdout. Non-"dbg:" lines are the program's
// own output (KindApp). Port of the Python driver's parse_line.
func ParseLine(line string) Event {
	s := strings.TrimRight(line, "\n")
	if !strings.HasPrefix(s, "dbg:") {
		return Event{Kind: KindApp, Text: s}
	}
	if s == "dbg:ready" {
		return Event{Kind: KindReady}
	}
	if m := pausedRe.FindStringSubmatch(s); m != nil {
		id, _ := strconv.Atoi(m[2])
		off, _ := strconv.Atoi(m[3])
		return Event{Kind: KindPaused, Mode: m[1], ID: id, Off: off}
	}
	if strings.HasPrefix(s, "dbg:stack off=") {
		if om := offRe.FindStringSubmatch(s); om != nil {
			off, _ := strconv.Atoi(om[1])
			regs := map[int]string{}
			for _, rm := range regRe.FindAllStringSubmatch(s, -1) {
				idx, _ := strconv.Atoi(rm[1])
				regs[idx] = rm[2]
			}
			return Event{Kind: KindStack, Off: off, Regs: regs}
		}
	}
	if strings.HasPrefix(s, "dbg:ok ") {
		return Event{Kind: KindOK, Verb: s[len("dbg:ok "):]}
	}
	if strings.HasPrefix(s, "dbg:error ") {
		return Event{Kind: KindError, Msg: s[len("dbg:error "):]}
	}
	return Event{Kind: KindOther, Text: s}
}

// Method is one entry of the VM's numeric method registry.
type Method struct {
	EntryBci int
	Arity    int
}

var methodLineRe = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+(\d+)\s*$`)

// ParseMethods parses the dbg:methods registry block into {id: Method}. The VM
// emits one "<id> <entry_bci> <arity>" line per method (NOT dbg:-prefixed);
// any line that is not exactly three whitespace-separated integers is skipped.
func ParseMethods(block string) map[int]Method {
	methods := map[int]Method{}
	for _, line := range strings.Split(block, "\n") {
		m := methodLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		id, _ := strconv.Atoi(m[1])
		entry, _ := strconv.Atoi(m[2])
		arity, _ := strconv.Atoi(m[3])
		methods[id] = Method{EntryBci: entry, Arity: arity}
	}
	return methods
}
