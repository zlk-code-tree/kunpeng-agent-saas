package architecture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// TestDomainModulesDoNotImportEachOther 防止领域模块绕过端口直接依赖其他领域实现。
// 跨领域协作必须在调用方声明接口，并由 app 装配适配器；该约束可以避免模块单体再次退化为分层大包。
func TestDomainModulesDoNotImportEachOther(t *testing.T) {
	domains := []string{
		"agent", "billing", "execution", "identity", "installation", "integration",
		"knowledge", "operations", "tenant", "toolgateway",
	}
	domainSet := make(map[string]struct{}, len(domains))
	for _, domain := range domains {
		domainSet[domain] = struct{}{}
	}
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("无法定位架构测试文件")
	}
	internalRoot := filepath.Dir(filepath.Dir(currentFile))

	for _, domain := range domains {
		domainDir := filepath.Join(internalRoot, domain)
		entries, err := os.ReadDir(domainDir)
		if err != nil {
			t.Fatalf("读取领域模块 %s 失败: %v", domain, err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			filename := filepath.Join(domainDir, entry.Name())
			file, err := parser.ParseFile(token.NewFileSet(), filename, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("解析领域模块文件 %s 失败: %v", filename, err)
			}
			for _, imported := range file.Imports {
				path, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatalf("解析导入路径失败: %v", err)
				}
				const prefix = "gin-dify-saas/server/internal/"
				if !strings.HasPrefix(path, prefix) {
					continue
				}
				target := strings.Split(strings.TrimPrefix(path, prefix), "/")[0]
				if _, isDomain := domainSet[target]; isDomain && target != domain {
					t.Errorf("领域模块 %s 不应在 %s 中直接导入领域模块 %s；请通过端口和 app 适配器协作", domain, filename, target)
				}
			}
		}
	}
}
