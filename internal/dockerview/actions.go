package dockerview

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
)

func validTarget(target string) bool {
	prefix, id, ok := strings.Cut(target, ":")
	if !ok || len(prefix) != 16 || !validID(id) {
		return false
	}
	_, err := hex.DecodeString(prefix)
	return err == nil
}

func offeredAction(row container) string {
	switch row.Status {
	case "running":
		return "restart"
	case "created", "exited":
		return "start"
	}
	return ""
}

func (c client) act(ctx context.Context, op, target string) ([]byte, error) {
	fail := func(en, zh string) ([]byte, error) { return json.Marshal(map[string]any{"error": words(en, zh)}) }
	_, id, _ := strings.Cut(target, ":")
	if c.itemID(id) != target {
		return fail("The Docker connection changed. Refresh before acting on this container.", "Docker 连接已变更。请刷新后再操作这个容器。")
	}
	rows, err := c.inspect(ctx, []string{id})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return fail("Could not re-read this container: "+err.Error(), "无法重新读取这个容器："+err.Error())
	}
	if offeredAction(rows[0]) != op {
		return fail("This container no longer offers that action. Refresh and try again.", "这个容器当前已不提供该操作，请刷新后重试。")
	}
	// Keep Docker's configured stop timeout. Ending a CLI request cannot cancel
	// an operation already accepted by the engine, so a timeout is uncertain.
	_, err = c.command(ctx, 4096, false, "container", op, "--", id)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return fail("Docker did not confirm the result in time. The engine may still be completing the action; refresh to check its state.", "Docker 未在时限内确认结果，引擎可能仍在执行操作；请刷新确认容器状态。")
		}
		return fail("Docker could not complete the action: "+err.Error(), "Docker 未能完成操作："+err.Error())
	}
	label := words("Started", "已启动")
	if op == "restart" {
		label = words("Restarted", "已重启")
	}
	return json.Marshal(map[string]any{"message": words(label["en"]+" "+strings.TrimPrefix(rows[0].Name, "/"), label["zh-CN"]+" "+strings.TrimPrefix(rows[0].Name, "/"))})
}
