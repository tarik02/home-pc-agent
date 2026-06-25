package runner

import (
	"context"
	"strings"

	"go.uber.org/zap"
)

func (a ActionConfig) effectiveRefreshTags() []string {
	if len(a.RefreshTags) > 0 {
		return a.RefreshTags
	}
	return a.Tags
}

func (p *Plugin) refreshAllGetterStates(ctx context.Context) {
	p.refreshAllDiscoveredOptions(ctx)

	p.mu.Lock()
	runtimes := make([]*actionRuntime, 0, len(p.runtimes))
	for _, runtime := range p.runtimes {
		if runtime.action.State.Source == stateGetter {
			runtimes = append(runtimes, runtime)
		}
	}
	p.mu.Unlock()

	for _, runtime := range runtimes {
		if err := p.refreshGetterState(ctx, runtime); err != nil {
			p.logger.Warn("runner startup getter refresh failed", zap.String("entity_id", runtime.entityID), zap.Error(err))
		}
	}
}

func (p *Plugin) refreshAllDiscoveredOptions(ctx context.Context) {
	p.mu.Lock()
	runtimes := make([]*actionRuntime, 0, len(p.runtimes))
	for _, runtime := range p.runtimes {
		if runtime.action.hasOptionsGetter() {
			runtimes = append(runtimes, runtime)
		}
	}
	p.mu.Unlock()

	for _, runtime := range runtimes {
		if err := p.refreshDiscoveredOptions(ctx, runtime); err != nil {
			p.logger.Warn("runner startup options refresh failed", zap.String("entity_id", runtime.entityID), zap.Error(err))
		}
	}
}

func (p *Plugin) refreshByTags(ctx context.Context, tags []string) {
	tagSet := normalizedTagSet(tags)
	if len(tagSet) == 0 {
		return
	}

	p.mu.Lock()
	optionTargets := make([]*actionRuntime, 0)
	stateTargets := make([]*actionRuntime, 0)
	for _, runtime := range p.runtimes {
		if !actionMatchesTagSet(runtime.action.Tags, tagSet) {
			continue
		}
		if runtime.action.hasOptionsGetter() {
			optionTargets = append(optionTargets, runtime)
		}
		if runtime.action.State.Source == stateGetter {
			stateTargets = append(stateTargets, runtime)
		}
	}
	p.mu.Unlock()

	for _, runtime := range optionTargets {
		if err := p.refreshDiscoveredOptions(ctx, runtime); err != nil {
			p.logger.Debug("runner tagged options refresh failed", zap.String("entity_id", runtime.entityID), zap.Strings("tags", tags), zap.Error(err))
		}
	}
	for _, runtime := range stateTargets {
		if err := p.refreshGetterState(ctx, runtime); err != nil {
			p.logger.Debug("runner tagged getter refresh failed", zap.String("entity_id", runtime.entityID), zap.Strings("tags", tags), zap.Error(err))
		}
	}
}

func (p *Plugin) refreshGetterState(ctx context.Context, runtime *actionRuntime) error {
	value, err := p.runGetter(ctx, runtime)
	if err != nil {
		return err
	}
	p.setLastSuccess(runtime.entityID, value)
	return p.host.PublishState(runtime.entityID, StateEnvelope{"state": formatHAState(runtime.action.Kind, value)})
}

func normalizedTagSet(tags []string) map[string]struct{} {
	set := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		tag = strings.ToLower(strings.TrimSpace(tag))
		if tag == "" {
			continue
		}
		set[tag] = struct{}{}
	}
	return set
}

func actionMatchesTagSet(actionTags []string, tagSet map[string]struct{}) bool {
	for _, tag := range actionTags {
		if _, ok := tagSet[strings.ToLower(strings.TrimSpace(tag))]; ok {
			return true
		}
	}
	return false
}
