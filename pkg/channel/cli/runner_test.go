package cli_test

import (
	"testing"

	"mini-code/pkg/channel"
	"mini-code/pkg/channel/cli"
)

// 编译期确认 CLIChannel 实现了 Channel 接口
var _ channel.Channel = (*cli.CLIChannel)(nil)

func TestCLIChannel_ChannelID(t *testing.T) {
	ch := cli.New(nil) // nil readline config，仅测试 ID
	if ch.ChannelID() != "cli" {
		t.Errorf("expected ChannelID 'cli', got %q", ch.ChannelID())
	}
}