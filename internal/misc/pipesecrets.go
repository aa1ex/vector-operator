package misc

import (
	"k8s.io/apimachinery/pkg/types"
	"maps"
	"sync"
)

type SecretsToPipelines struct {
	mu                sync.RWMutex
	secretToPipelines map[types.NamespacedName]map[types.NamespacedName]struct{}
	pipelineToSecrets map[types.NamespacedName]map[types.NamespacedName]struct{}
}

func NewSecretsToPipelines() *SecretsToPipelines {
	return &SecretsToPipelines{
		mu:                sync.RWMutex{},
		secretToPipelines: make(map[types.NamespacedName]map[types.NamespacedName]struct{}),
		pipelineToSecrets: make(map[types.NamespacedName]map[types.NamespacedName]struct{}),
	}
}

func (sp *SecretsToPipelines) Get(secret types.NamespacedName) []types.NamespacedName {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	data := sp.secretToPipelines[secret]
	list := make([]types.NamespacedName, 0, len(data))
	for k := range data {
		list = append(list, k)
	}
	return list
}

func (sp *SecretsToPipelines) Add(secret types.NamespacedName, pipeline types.NamespacedName) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.secretToPipelines[secret] == nil {
		sp.secretToPipelines[secret] = make(map[types.NamespacedName]struct{})
	}
	sp.secretToPipelines[secret][pipeline] = struct{}{}
	if sp.pipelineToSecrets[pipeline] == nil {
		sp.pipelineToSecrets[pipeline] = make(map[types.NamespacedName]struct{})
	}
	sp.pipelineToSecrets[pipeline][secret] = struct{}{}
}

func (sp *SecretsToPipelines) Delete(secret types.NamespacedName, pipeline types.NamespacedName) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if sp.secretToPipelines[secret] != nil {
		delete(sp.secretToPipelines[secret], pipeline)
	}
	if len(sp.secretToPipelines[secret]) == 0 {
		delete(sp.secretToPipelines, secret)
	}
	if sp.pipelineToSecrets[pipeline] != nil {
		delete(sp.pipelineToSecrets[pipeline], secret)
	}
	if len(sp.pipelineToSecrets[pipeline]) == 0 {
		delete(sp.pipelineToSecrets, pipeline)
	}
}

func (sp *SecretsToPipelines) GetPipelineSecretsSet(pipeline types.NamespacedName) map[types.NamespacedName]struct{} {
	sp.mu.RLock()
	defer sp.mu.RUnlock()
	secrets, ok := sp.pipelineToSecrets[pipeline]
	if !ok {
		return make(map[types.NamespacedName]struct{})
	}
	return maps.Clone(secrets)
}
