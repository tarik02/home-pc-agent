//go:build windows

package fancontrol

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func TestParseFanControlIPCReplies(t *testing.T) {
	listPayload, err := proto.Marshal(&ListAvailableConfigsReply{
		Configs:       []string{"terra-balanced.json", "terra-quiet.json"},
		CurrentConfig: "terra-quiet.json",
		ConfigFolder:  `C:\Program Files (x86)\FanControl\Configurations`,
	})
	require.NoError(t, err)

	configs, err := parseListAvailableConfigsReply(listPayload)
	require.NoError(t, err)
	require.Equal(t, []string{"terra-balanced.json", "terra-quiet.json"}, configs.Configs)
	require.Equal(t, "terra-quiet.json", configs.CurrentConfig)
	require.Equal(t, `C:\Program Files (x86)\FanControl\Configurations`, configs.ConfigFolder)

	commandPayload, err := proto.Marshal(&CommandReply{Status: CommandStatus_COMMAND_STATUS_OK, User: "alice"})
	require.NoError(t, err)
	status, user, err := parseCommandReply(commandPayload)
	require.NoError(t, err)
	require.Equal(t, CommandStatus_COMMAND_STATUS_OK, status)
	require.Equal(t, "alice", user)
}

func TestFanControlIPCLive(t *testing.T) {
	if os.Getenv("HOMEPC_FANCONTROL_IPC_LIVE") == "" {
		t.Skip("set HOMEPC_FANCONTROL_IPC_LIVE=1 to probe a running FanControl instance")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, err := openFanControlIPC(ctx)
	require.NoError(t, err)
	defer conn.Close()

	rpc := newFanControlRPC(conn)
	configs, err := rpc.ListConfigs(ctx)
	require.NoError(t, err)
	require.NotEmpty(t, configs.Configs)
	require.NotEmpty(t, configs.CurrentConfig)

	if target := os.Getenv("HOMEPC_FANCONTROL_IPC_LOAD"); target != "" {
		loadConn, err := openFanControlIPC(ctx)
		require.NoError(t, err)
		loadRPC := newFanControlRPC(loadConn)
		require.NoError(t, loadRPC.LoadConfig(ctx, target))
		require.NoError(t, loadConn.Close())

		afterConn, err := openFanControlIPC(ctx)
		require.NoError(t, err)
		defer afterConn.Close()
		after, err := newFanControlRPC(afterConn).ListConfigs(ctx)
		require.NoError(t, err)
		require.Equal(t, target, after.CurrentConfig)
	}
}
