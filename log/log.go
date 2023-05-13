package log

import (
	"fmt"
	"log"
)

// WarnFunc is called on a warning.
type WarnFunc func(string)

var warnFunc WarnFunc = func(s string) {
	log.Println("go-qcow2reader: WARNING: " + s)
}

// SetWarnFunc sets [WarnFunc].
func SetWarnFunc(fn WarnFunc) {
	warnFunc = fn
}

// Warn prints a warning.
func Warn(a ...any) {
	if warnFunc != nil {
		warnFunc(fmt.Sprint(a...))
	}
}

// Warnf prints a warning.
func Warnf(format string, a ...any) {
	Warn(fmt.Sprintf(format, a...))
}

// DebugFunc is called for debug prints (very verbose).
type DebugFunc func(string)

var debugPrintFunc DebugFunc

// SetDebugFunc sets [DebugFunc].
func SetDebugFunc(fn DebugFunc) {
	debugPrintFunc = fn
}

// Debug prints a debug message.
func Debug(a ...any) {
	if debugPrintFunc != nil {
		debugPrintFunc(fmt.Sprint(a...))
	}
}

// Debugf prints a debug message.
func Debugf(format string, a ...any) {
	Debug(fmt.Sprintf(format, a...))
}
