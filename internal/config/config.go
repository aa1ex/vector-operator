/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package config

import (
	"encoding/json"
	"errors"
	"strconv"

	vectorv1alpha1 "github.com/kaasops/vector-operator/api/v1alpha1"
	"github.com/kaasops/vector-operator/internal/vector/vectoragent"
	"github.com/mitchellh/mapstructure"
)

var (
	ErrNotAllowedSourceType   error = errors.New("type kubernetes_logs only allowed")
	ErrClusterScopeNotAllowed error = errors.New("logs from external namespace not allowed")
)

func newVectorConfig(apiEnabled, playgroundEnabled bool) *VectorConfig {
	sources := make(map[string]*Source)
	transforms := make(map[string]*Transform)
	sinks := make(map[string]*Sink)

	api := &ApiSpec{
		Address:    "0.0.0.0:" + strconv.Itoa(vectoragent.ApiPort),
		Enabled:    apiEnabled,
		Playground: playgroundEnabled,
	}

	return &VectorConfig{
		DataDir: "/vector-data-dir",
		Api:     api,
		PipelineConfig: PipelineConfig{
			Sources:    sources,
			Transforms: transforms,
			Sinks:      sinks,
		},
	}
}

func UnmarshalJson(spec vectorv1alpha1.VectorPipelineSpec, p *PipelineConfig) error {
	b, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	pipeline_ := &pipelineConfig_{}
	if err := json.Unmarshal(b, pipeline_); err != nil {
		return err
	}
	if err := mapstructure.Decode(pipeline_.Sources, &p.Sources); err != nil {
		return err
	}
	if err := mapstructure.Decode(pipeline_.Transforms, &p.Transforms); err != nil {
		return err
	}
	if err := mapstructure.Decode(pipeline_.Sinks, &p.Sinks); err != nil {
		return err
	}
	return nil
}

func isExporterSinkExists(sinks map[string]*Sink) bool {
	for _, sink := range sinks {
		if sink.Type == InternalMetricsSinkType {
			return true
		}
	}
	return false
}
