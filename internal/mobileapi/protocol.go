package mobileapi

import (
	"errors"

	"daily-log/internal/model"
)

var ErrSession = errors.New("配对会话已过期，请重新配对")
var ErrDisconnected = errors.New("采集主机暂时无法访问。可保留当前输入，重连后再保存")

type Problem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type SaveRequest struct {
	Day, Revision string
	Text          model.JournalText
	Confirm       bool
}

type ModelChoice struct {
	ID string
}

type CategoryChange struct {
	ID, Category string
}
