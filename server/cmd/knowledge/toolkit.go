package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
)

const toolkitSourcePrefix = "docker-toolkit/"

var secretAssignment = regexp.MustCompile(`(?im)^(\s*[A-Z][A-Z0-9_]*(?:PASSWORD|SECRET|TOKEN|API_KEY|COOKIE)[A-Z0-9_]*\s*=)\s*[^\r\n#]+`)
var sqlPassword = regexp.MustCompile(`(?i)(IDENTIFIED\s+(?:WITH\s+\S+\s+)?BY\s+)('[^']*'|"[^"]*")`)

// importToolkit 只索引编排仓库中的说明文档和公开模板。
// 实际运行的 compose.yml、.env、站点文件及项目源码都不进入知识库。
func (s *service) importToolkit(ctx context.Context, root string) error {
	var sources []string
	files, chunks := 0, 0
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if relative != "." && !toolkitDirectoryAllowed(relative) {
				return filepath.SkipDir
			}
			return nil
		}
		if !toolkitFileAllowed(relative) || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Size() > 256*1024 {
			return fmt.Errorf("toolkit document too large: %s", relative)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source := toolkitSourcePrefix + relative
		content := sanitizeToolkitText(string(body))
		prefix := "来源：Docker 容器编排项目 / " + relative + "\n"
		if strings.HasSuffix(relative, "sample.env") || strings.HasSuffix(relative, "compose.sample.yml") {
			prefix += "说明：这是示例模板；部署时应按目标项目修改，密码及令牌必须重新生成。\n"
		}
		count, err := s.importDocument(ctx, source, relative, content, prefix)
		if err != nil {
			return err
		}
		sources = append(sources, source)
		files++
		chunks += count
		slog.Info("indexed toolkit document", "source", relative, "chunks", count)
		return nil
	})
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return fmt.Errorf("no toolkit documents selected from %s", root)
	}
	// 成功遍历全部源文件后再移除旧版本已删除的模板，避免半途失败导致资料缺失。
	_, err = s.db.Exec(ctx, `DELETE FROM knowledge_chunks WHERE knowledge_id = $1
		AND source_path LIKE 'docker-toolkit/%' AND NOT (source_path = ANY($2))`, s.config.knowledgeID, sources)
	if err != nil {
		return err
	}
	slog.Info("toolkit import completed", "knowledge_id", s.config.knowledgeID, "files", files, "chunks", chunks)
	return nil
}

// toolkitDirectoryAllowed 采用显式目录白名单，防止遍历仓库的 www、config、日志和私有运行数据。
func toolkitDirectoryAllowed(relative string) bool {
	parts := strings.Split(relative, "/")
	switch parts[0] {
	case "acme", "elasticsearch", "etcd", "jenkins", "kibana", "memcached", "mongo", "mysql", "nginx", "node", "php", "postgres", "rabbitmq", "redis", "readme":
		for _, part := range parts[1:] {
			if part == "config" || part == "logs" || part == "data" || part == "extensions" || part == "vendor" || part == "node_modules" {
				return false
			}
		}
		return len(parts) <= 4
	default:
		return false
	}
}

// toolkitFileAllowed 只接受明确的说明文件、样例 Compose、样例环境变量及构建定义。
func toolkitFileAllowed(relative string) bool {
	base := filepath.Base(relative)
	if relative == "README.md" || relative == "compose.sample.yml" || relative == "sample.env" {
		return true
	}
	parts := strings.Split(relative, "/")
	if len(parts) < 2 || !toolkitDirectoryAllowed(strings.Join(parts[:len(parts)-1], "/")) {
		return false
	}
	return strings.HasSuffix(strings.ToLower(base), ".md") || base == "compose.sample.yml" || base == "sample.env" || base == "Dockerfile"
}

// sanitizeToolkitText 只处理索引副本，不修改原项目；隐藏示例密码避免模型复述弱口令。
func sanitizeToolkitText(content string) string {
	content = secretAssignment.ReplaceAllString(content, `${1}<请设置强随机值>`)
	content = sqlPassword.ReplaceAllString(content, `${1}'<新密码>'`)
	return content
}

// importDocument 在向量编码全部完成后才提交替换，确保失败时旧索引仍可检索。
func (s *service) importDocument(ctx context.Context, source, title, content, prefix string) (int, error) {
	parts := splitMarkdown(content, 850, 100)
	if len(parts) == 0 {
		return 0, nil
	}
	for index := range parts {
		parts[index] = prefix + parts[index]
	}
	vectors := make([][]float64, 0, len(parts))
	for start := 0; start < len(parts); start += 8 {
		end := start + 8
		if end > len(parts) {
			end = len(parts)
		}
		batch, err := s.embed(ctx, parts[start:end], false)
		if err != nil {
			return 0, fmt.Errorf("embed %s: %w", source, err)
		}
		vectors = append(vectors, batch...)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(ctx, `DELETE FROM knowledge_chunks WHERE knowledge_id = $1 AND source_path = $2`, s.config.knowledgeID, source)
	if err == nil {
		for index, part := range parts {
			_, err = tx.Exec(ctx, `INSERT INTO knowledge_chunks (knowledge_id, source_path, title, content, embedding, chunk_index)
				VALUES ($1,$2,$3,$4,$5::vector,$6)`, s.config.knowledgeID, source, title, part, vectorLiteral(vectors[index]), index)
			if err != nil {
				break
			}
		}
	}
	if err != nil {
		_ = tx.Rollback(ctx)
		return 0, fmt.Errorf("store %s: %w", source, err)
	}
	if err := tx.Commit(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return 0, err
	}
	return len(parts), nil
}
