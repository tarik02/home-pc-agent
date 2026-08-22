package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWatcherNotifiesOnWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "home-pc-agent.toml")
	require.NoError(t, os.WriteFile(path, []byte("enabled = true\n"), 0o600))

	notify := make(chan struct{}, 1)
	watcher, err := Watch(path, func() {
		select {
		case notify <- struct{}{}:
		default:
		}
	})
	require.NoError(t, err)
	defer func() {
		require.NoError(t, watcher.Close())
	}()

	require.NoError(t, os.WriteFile(path, []byte("enabled = false\n"), 0o600))

	select {
	case <-notify:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for config change notification")
	}
}
