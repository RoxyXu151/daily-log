package runtimeapp

import (
	"context"
	"fmt"

	"daily-log/internal/app"
	"daily-log/internal/config"
	"daily-log/internal/source"
)

type Probe func(context.Context) (string, error)

// SourceRegistration connects source metadata to a collector and a read-only probe.
type SourceRegistration struct {
	Spec config.SourceSpec
	Open func(config.Config) (source.Collector, Probe, error)
}

func (r SourceRegistration) open(cfg config.Config) (source.Collector, Probe, error) {
	if r.Open == nil {
		return nil, nil, fmt.Errorf("来源 %s 缺少连接构造器", r.Spec.ID)
	}
	collect, probe, err := r.Open(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("来源 %s：%w", r.Spec.ID, err)
	}
	if collect == nil || probe == nil {
		return nil, nil, fmt.Errorf("来源 %s 缺少采集或测试实现", r.Spec.ID)
	}
	return collect, probe, nil
}

func (w Workspace) loadConfig() (config.Config, error) {
	specs := make([]config.SourceSpec, 0, len(w.Connectors))
	for _, registration := range w.Connectors {
		specs = append(specs, registration.Spec)
	}
	return config.Load(w.ConfigPath, specs...)
}

func (i Integrations) configureConnectors(s *app.Service, registrations []SourceRegistration) {
	if len(registrations) == 0 {
		return
	}
	options := i.config.Sources()
	for _, registration := range registrations {
		id := registration.Spec.ID
		option := options[id]
		if !option.Enabled {
			continue
		}
		s.Inputs = append(s.Inputs, app.Input{Name: id, Required: option.IsRequired(), Collect: func(ctx context.Context, window source.Window) (source.Batch, error) {
			collect, _, err := registration.open(i.config)
			if err != nil {
				return source.Batch{}, err
			}
			return collect(ctx, window)
		}})
	}
}
