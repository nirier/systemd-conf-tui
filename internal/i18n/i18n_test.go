package i18n

import (
	"errors"
	"strings"
	"testing"
)

func TestLocalizedErrorsPreserveArgumentsAndWrapping(t *testing.T) {
	cause := errors.New("原始错误 / original error")
	err := Errorf("unit 校验失败，尚未安装: %w", Errorf("无法创建 %s（已有文件不会覆盖）: %w", "/tmp/服务.service", cause))
	if !errors.Is(err, cause) {
		t.Fatal("error wrapping lost")
	}
	if Chinese.Error(err) != err.Error() {
		t.Fatal("Chinese error behavior changed")
	}
	translated := English.Error(err)
	for _, want := range []string{"Unit validation failed; not installed", "Cannot create /tmp/服务.service", cause.Error()} {
		if !strings.Contains(translated, want) {
			t.Fatalf("translation lost message or literal argument: %s", translated)
		}
	}
	if strings.Contains(translated, "%!") {
		t.Fatal("translated format is invalid", translated)
	}
	if English.Text("unknown message") != "unknown message" || English.Error(nil) != "" {
		t.Fatal("fallback changed")
	}
}
