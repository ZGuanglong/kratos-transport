package signalr

import "github.com/tx7do/kratos-transport/log"

type logger struct {
	log.Logger
}

func (l *logger) Log(keyVals ...interface{}) error {
	return l.Logger.Log(log.LevelError, keyVals...)
}
