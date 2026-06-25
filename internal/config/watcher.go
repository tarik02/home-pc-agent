package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const defaultReloadDebounce = 500 * time.Millisecond

type Watcher struct {
	path     string
	base     string
	debounce time.Duration
	onChange func()

	watcher *fsnotify.Watcher
	done    chan struct{}
	once    sync.Once
}

func Watch(path string, onChange func()) (*Watcher, error) {
	if path == "" {
		return nil, fmt.Errorf("config path is required")
	}
	if onChange == nil {
		return nil, fmt.Errorf("onChange callback is required")
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, fmt.Errorf("stat config %q: %w", abs, err)
	}

	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("create config watcher: %w", err)
	}

	w := &Watcher{
		path:     abs,
		base:     filepath.Base(abs),
		debounce: defaultReloadDebounce,
		onChange: onChange,
		watcher:  fs,
		done:     make(chan struct{}),
	}
	if err := fs.Add(filepath.Dir(abs)); err != nil {
		_ = fs.Close()
		return nil, fmt.Errorf("watch config directory: %w", err)
	}

	go w.loop()
	return w, nil
}

func (w *Watcher) Close() error {
	var err error
	w.once.Do(func() {
		close(w.done)
		err = w.watcher.Close()
	})
	return err
}

func (w *Watcher) loop() {
	var (
		mu     sync.Mutex
		timer  *time.Timer
		notify = func() {
			mu.Lock()
			defer mu.Unlock()
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(w.debounce, w.onChange)
		}
	)

	for {
		select {
		case <-w.done:
			mu.Lock()
			if timer != nil {
				timer.Stop()
			}
			mu.Unlock()
			return
		case event, ok := <-w.watcher.Events:
			if !ok {
				return
			}
			if filepath.Base(event.Name) != w.base {
				continue
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) || event.Has(fsnotify.Rename) {
				notify()
			}
		case _, ok := <-w.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}
