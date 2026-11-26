package main

import (
	"context"

	"daily-log/internal/config"
	"daily-log/internal/model"
	"daily-log/internal/runtimeapp"
	"daily-log/internal/source"
)

func fixtureSource(event *model.Event) runtimeapp.SourceRegistration {
	return runtimeapp.SourceRegistration{
		Spec: config.SourceSpec{ID: "fixture", Label: "测试来源", Description: "测试活动证据"},
		Open: func(config.Config) (source.Collector, runtimeapp.Probe, error) {
			return func(context.Context, source.Window) (source.Batch, error) {
					return source.Batch{Events: []model.Event{*event}}, nil
				}, func(context.Context) (string, error) {
					return "测试来源可用", nil
				}, nil
		},
	}
}
