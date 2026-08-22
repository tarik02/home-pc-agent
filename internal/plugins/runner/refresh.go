package runner

import (
	"context"
	"strings"

	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const refreshConcurrency = 4

func (a ActionConfig) effectiveRefreshTags() []string {
	if len(a.RefreshTags) > 0 {
		return a.RefreshTags
	}
	return a.Tags
}

func (p *Plugin) refreshByTags(ctx context.Context, tags []string) {
	tagSet := normalizedTagSet(tags)
	if len(tagSet) == 0 {
		return
	}

	p.mu.RLock()
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
	p.mu.RUnlock()

	refreshConcurrently(ctx, optionTargets, func(refreshCtx context.Context, runtime *actionRuntime) {
		if err := p.refreshDiscoveredOptions(refreshCtx, runtime); err != nil {
			p.logger.Debug("runner tagged options refresh failed", zap.String("entity_id", runtime.entityID), zap.Strings("tags", tags), zap.Error(err))
		}
	})
	refreshConcurrently(ctx, stateTargets, func(refreshCtx context.Context, runtime *actionRuntime) {
		if err := p.refreshGetterState(refreshCtx, runtime); err != nil {
			p.logger.Debug("runner tagged getter refresh failed", zap.String("entity_id", runtime.entityID), zap.Strings("tags", tags), zap.Error(err))
		}
	})
}

func (p *Plugin) refreshGetterState(ctx context.Context, runtime *actionRuntime) error {
	value, err := p.runGetter(ctx, runtime)
	if err != nil {
		return err
	}
	return p.host.PublishState(runtime.entityID, StateEnvelope{"state": formatHAState(runtime.action.Kind, value)})
}

func refreshConcurrently(ctx context.Context, runtimes []*actionRuntime, refresh func(context.Context, *actionRuntime)) {
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(refreshConcurrency)
	for _, runtime := range runtimes {
		runtime := runtime
		group.Go(func() error {
			refresh(groupCtx, runtime)
			return nil
		})
	}
	_ = group.Wait()
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
