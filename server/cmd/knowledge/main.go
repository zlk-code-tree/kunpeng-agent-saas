package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

const embeddingDimensions = 512

type config struct {
	databaseURL string
	embedURL    string
	apiKey      string
	knowledgeID string
	addr        string
}

type service struct {
	config config
	db     *pgxpool.Pool
	client *http.Client
}

type retrievalRequest struct {
	KnowledgeID      string `json:"knowledge_id"`
	Query            string `json:"query"`
	RetrievalSetting struct {
		TopK           int     `json:"top_k"`
		ScoreThreshold float64 `json:"score_threshold"`
	} `json:"retrieval_setting"`
}

type record struct {
	Content  string         `json:"content"`
	Score    float64        `json:"score"`
	Title    string         `json:"title"`
	Metadata map[string]any `json:"metadata"`
}

// main 同时提供 Dify 外部知识库检索接口和两种幂等导入命令。
func main() {
	cfg := config{
		databaseURL: os.Getenv("KNOWLEDGE_DATABASE_URL"),
		embedURL:    strings.TrimRight(os.Getenv("EMBEDDING_URL"), "/"),
		apiKey:      os.Getenv("KNOWLEDGE_API_KEY"),
		knowledgeID: os.Getenv("KNOWLEDGE_ID"),
		addr:        os.Getenv("KNOWLEDGE_HTTP_ADDR"),
	}
	if cfg.databaseURL == "" || cfg.embedURL == "" || cfg.apiKey == "" || cfg.knowledgeID == "" {
		slog.Error("KNOWLEDGE_DATABASE_URL, EMBEDDING_URL, KNOWLEDGE_API_KEY and KNOWLEDGE_ID are required")
		os.Exit(1)
	}
	if cfg.addr == "" {
		cfg.addr = ":8080"
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	db, err := pgxpool.New(ctx, cfg.databaseURL)
	if err != nil {
		slog.Error("connect knowledge database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	s := &service{config: cfg, db: db, client: &http.Client{Timeout: 60 * time.Second}}
	if err := s.prepare(ctx); err != nil {
		slog.Error("prepare knowledge database", "error", err)
		os.Exit(1)
	}
	if len(os.Args) > 1 && (os.Args[1] == "import" || os.Args[1] == "import-toolkit") {
		if len(os.Args) != 3 {
			slog.Error("usage: knowledge import|import-toolkit /path/to/directory")
			os.Exit(2)
		}
		var err error
		if os.Args[1] == "import-toolkit" {
			err = s.importToolkit(ctx, os.Args[2])
		} else {
			err = s.importDirectory(ctx, os.Args[2])
		}
		if err != nil {
			slog.Error("import knowledge", "error", err)
			os.Exit(1)
		}
		return
	}
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.GET("/healthz", func(c *gin.Context) {
		if err := s.db.Ping(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	router.POST("/retrieval", s.retrieve)
	server := &http.Server{Addr: cfg.addr, Handler: router, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 90 * time.Second}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	slog.Info("knowledge gateway started", "address", cfg.addr, "knowledge_id", cfg.knowledgeID)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("knowledge gateway stopped", "error", err)
		os.Exit(1)
	}
}

// prepare 创建独立知识库的向量表；向量维度固定为 bge-small-zh-v1.5 的 512 维。
func (s *service) prepare(ctx context.Context) error {
	statements := []string{
		`CREATE EXTENSION IF NOT EXISTS vector`,
		`CREATE TABLE IF NOT EXISTS knowledge_chunks (
			id bigserial PRIMARY KEY,
			knowledge_id text NOT NULL,
			source_path text NOT NULL,
			title text NOT NULL,
			content text NOT NULL,
			embedding vector(512) NOT NULL,
			chunk_index integer NOT NULL,
			created_at timestamptz NOT NULL DEFAULT now(),
			UNIQUE (knowledge_id, source_path, chunk_index)
		)`,
		`CREATE INDEX IF NOT EXISTS knowledge_chunks_hnsw ON knowledge_chunks USING hnsw (embedding vector_cosine_ops)`,
		`CREATE INDEX IF NOT EXISTS knowledge_chunks_scope ON knowledge_chunks (knowledge_id)`,
	}
	for _, sql := range statements {
		if _, err := s.db.Exec(ctx, sql); err != nil {
			return err
		}
	}
	return nil
}

// embed 调用本地模型服务；索引和查询使用同一模型，查询额外添加 BGE 检索提示词。
func (s *service) embed(ctx context.Context, texts []string, query bool) ([][]float64, error) {
	body, err := json.Marshal(map[string]any{"texts": texts, "query": query})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.config.embedURL+"/embed", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		message, _ := io.ReadAll(io.LimitReader(res.Body, 1024))
		return nil, fmt.Errorf("embedding service returned %d: %s", res.StatusCode, strings.TrimSpace(string(message)))
	}
	var output struct {
		Vectors [][]float64 `json:"vectors"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&output); err != nil {
		return nil, err
	}
	if len(output.Vectors) != len(texts) {
		return nil, fmt.Errorf("embedding service returned %d vectors for %d texts", len(output.Vectors), len(texts))
	}
	for _, vector := range output.Vectors {
		if len(vector) != embeddingDimensions {
			return nil, fmt.Errorf("embedding dimension %d, want %d", len(vector), embeddingDimensions)
		}
	}
	return output.Vectors, nil
}

// retrieve 实现 Dify External Knowledge API：先验证密钥和知识库范围，再执行向量检索。
func (s *service) retrieve(c *gin.Context) {
	authorization := c.GetHeader("Authorization")
	provided := strings.TrimPrefix(authorization, "Bearer ")
	if !strings.HasPrefix(authorization, "Bearer ") || subtle.ConstantTimeCompare([]byte(provided), []byte(s.config.apiKey)) != 1 {
		c.JSON(http.StatusForbidden, gin.H{"error": "invalid API key"})
		return
	}
	var input retrievalRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}
	// Dify 创建外部 API 时会发送空 knowledge_id 和空 query 进行连通性探测。
	if input.KnowledgeID == "" && input.Query == "" {
		c.JSON(http.StatusOK, gin.H{"records": []record{}})
		return
	}
	if input.KnowledgeID != s.config.knowledgeID {
		c.JSON(http.StatusNotFound, gin.H{"error": "knowledge base not found"})
		return
	}
	query := strings.TrimSpace(input.Query)
	if query == "" || len([]rune(query)) > 2048 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "query must be 1-2048 characters"})
		return
	}
	topK := input.RetrievalSetting.TopK
	if topK < 1 {
		topK = 4
	}
	if topK > 20 {
		topK = 20
	}
	threshold := input.RetrievalSetting.ScoreThreshold
	if math.IsNaN(threshold) || threshold < 0 || threshold > 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid score_threshold"})
		return
	}
	vectors, err := s.embed(c.Request.Context(), []string{query}, true)
	if err != nil {
		slog.Error("embed query", "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "embedding service unavailable"})
		return
	}
	rows, err := s.db.Query(c.Request.Context(), `SELECT content, title, source_path, 1 - (embedding <=> $2::vector) AS score
		FROM knowledge_chunks WHERE knowledge_id = $1
		ORDER BY embedding <=> $2::vector LIMIT 160`, input.KnowledgeID, vectorLiteral(vectors[0]))
	if err != nil {
		slog.Error("search vectors", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "retrieval failed"})
		return
	}
	defer rows.Close()
	candidates := make([]retrievalCandidate, 0, 160)
	for rows.Next() {
		var content, title, source string
		var score float64
		if err := rows.Scan(&content, &title, &source, &score); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "retrieval failed"})
			return
		}
		candidates = append(candidates, retrievalCandidate{content: content, title: title, source: source, vectorScore: score})
	}
	if err := rows.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "retrieval failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"records": rankCandidates(query, candidates, topK, threshold)})
}

// importDirectory 遍历项目部署文档；每份文件先完整编码，再在事务中替换旧片段。
// 某份文件编码失败不会删掉它已有的可检索内容。
func (s *service) importDirectory(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	files, chunks := 0, 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") || strings.EqualFold(entry.Name(), "README.md") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		count, err := s.importDocument(ctx, entry.Name(), strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())), string(content), "")
		if err != nil {
			return err
		}
		files++
		chunks += count
		slog.Info("indexed document", "name", entry.Name(), "chunks", count)
	}
	slog.Info("knowledge import completed", "knowledge_id", s.config.knowledgeID, "files", files, "chunks", chunks)
	return nil
}

// vectorLiteral 使用 pgvector 的文本表示，作为参数传递而非拼接进 SQL。
func vectorLiteral(values []float64) string {
	var builder strings.Builder
	builder.WriteByte('[')
	for i, value := range values {
		if i != 0 {
			builder.WriteByte(',')
		}
		builder.WriteString(strconv.FormatFloat(value, 'f', 8, 64))
	}
	builder.WriteByte(']')
	return builder.String()
}

// splitMarkdown 按段落切片并保留少量重叠文本，使跨段落问题仍能命中。
func splitMarkdown(source string, maxRunes, overlap int) []string {
	text := strings.TrimSpace(strings.ReplaceAll(source, "\r\n", "\n"))
	if text == "" {
		return nil
	}
	runes := []rune(text)
	var chunks []string
	for start := 0; start < len(runes); {
		end := start + maxRunes
		if end >= len(runes) {
			chunks = append(chunks, strings.TrimSpace(string(runes[start:])))
			break
		}
		// 尽量在段落边界截断，避免切断 Markdown 语句；过短时仍按最大长度切。
		for candidate := end; candidate > start+maxRunes/2; candidate-- {
			if runes[candidate-1] == '\n' {
				end = candidate
				break
			}
		}
		part := strings.TrimSpace(string(runes[start:end]))
		if part != "" {
			chunks = append(chunks, part)
		}
		start = end - overlap
		if start < 0 {
			start = 0
		}
	}
	return chunks
}
