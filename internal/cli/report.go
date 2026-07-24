package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/xunull/inhomo/internal/ai"
	"github.com/xunull/inhomo/internal/store"
)

// reportData 是喂给 LLM 的**聚合**素材：全是计数与归属/节点/地区标签，**不含原始访问域名**（见 ADR-0012）。
type reportData struct {
	Since    string
	Trackers TrackerBreakdown
	Nodes    []store.AggRow // 出境节点 top（by=node）
	Regions  []store.AggRow // 节点地区 top（by=region）
}

func newReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "用 AI 把连接态势总结成自然语言隐私周报（查询运行中的 serve，只发聚合、不发原始域名）",
		Args:  cobra.NoArgs,
		RunE:  runReport,
	}
	cmd.Flags().String(flagAddr, "127.0.0.1:8566", "要查询的运行中 serve 地址（report 从它的 /api 取聚合）")
	cmd.Flags().String("since", "7d", "统计时间窗（如 7d / 24h）")
	cmd.Flags().String("out", "", "把报告写入该文件（留空则打印到终端）")
	cmd.Flags().String("ai-provider", "anthropic", "AI 提供方：anthropic 或 openai（openai 兼容 DeepSeek/OpenAI/Groq/Ollama 等）")
	cmd.Flags().String("ai-model", "claude-sonnet-5", "生成用的模型（换 provider 时也要换：如 DeepSeek 用 deepseek-chat）")
	cmd.Flags().String("ai-api-key", "", "API key（建议用 INHOMO_AI_API_KEY 环境变量或配置文件，勿写进命令行历史）")
	cmd.Flags().String("ai-base-url", "", "API 基址（默认各 provider 官方；DeepSeek 用 https://api.deepseek.com）")
	return cmd
}

func runReport(cmd *cobra.Command, _ []string) error {
	v := cfgOf(cmd)
	addr := v.GetString(flagAddr)
	since := v.GetString("since")
	if _, err := parseDur(since); err != nil {
		return fmt.Errorf("无效的 since %q：%w", since, err)
	}
	apiKey := v.GetString("ai-api-key")
	if apiKey == "" {
		return fmt.Errorf("未配置 Anthropic API key（设 INHOMO_AI_API_KEY 环境变量，或 ~/.inhomo/config.yaml 的 ai-api-key）")
	}
	model := v.GetString("ai-model")
	outPath := v.GetString("out")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	data, err := fetchReportData(ctx, addr, since)
	if err != nil {
		return fmt.Errorf("从 serve %s 取聚合失败（serve 在跑吗？）：%w", addr, err)
	}

	provider, err := ai.New(v.GetString("ai-provider"), apiKey, model, v.GetString("ai-base-url"))
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "[inhomo] 生成隐私周报（近 %s，%s/%s）…\n", since, v.GetString("ai-provider"), model)
	text, err := provider.Generate(ctx, buildReportPrompt(data))
	if err != nil {
		return fmt.Errorf("生成失败：%w", err)
	}
	text = strings.TrimSpace(text) + "\n"

	if outPath != "" {
		if err := os.WriteFile(outPath, []byte(text), 0o644); err != nil {
			return fmt.Errorf("写入 %s：%w", outPath, err)
		}
		fmt.Fprintf(os.Stderr, "[inhomo] 报告已写入 %s\n", outPath)
		return nil
	}
	fmt.Print(text)
	return nil
}

// fetchReportData 从运行中的 serve 的 /api 取聚合（避开 DuckDB 单写锁：不直接开库，复用 serve 已开的库）。
// 只取聚合接口（trackers / by=node / by=region）——都不含原始访问域名。
func fetchReportData(ctx context.Context, addr, since string) (reportData, error) {
	base := "http://" + addr
	q := "since=" + url.QueryEscape(since) + "&limit=8"
	client := &http.Client{Timeout: 10 * time.Second}
	d := reportData{Since: since}
	if err := fetchJSON(ctx, client, base+"/api/trackers?"+q, &d.Trackers); err != nil {
		return d, err
	}
	if err := fetchJSON(ctx, client, base+"/api/aggregate?by=node&"+q, &d.Nodes); err != nil {
		return d, err
	}
	if err := fetchJSON(ctx, client, base+"/api/aggregate?by=region&"+q, &d.Regions); err != nil {
		return d, err
	}
	return d, nil
}

func fetchJSON(ctx context.Context, client *http.Client, u string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s 返回 %s", u, resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(target); err != nil {
		return fmt.Errorf("解码 %s：%w", u, err)
	}
	return nil
}

// labeledCount 是「标签 → 计数」的一行，供报告里各排名统一渲染。
type labeledCount struct {
	label string
	count int64
}

// buildReportPrompt 把聚合素材拼成中文提示词（纯函数、可测）。只放计数与归属/节点/地区标签，
// **不含任何原始访问域名**——隐私由 reportData 的构造保证（不取 by=host 等含域名的接口）。
func buildReportPrompt(d reportData) string {
	var b strings.Builder
	b.WriteString("你是隐私分析助手。根据下面这台机器近期经 mihomo 出站连接的**聚合统计**，写一份简洁的中文隐私周报：" +
		"先一句话总体态势，再点出最值得注意的追踪器暴露与出境节点，最后给 2-3 条可行动建议。" +
		"只依据给出的数字，不要编造具体域名。\n\n")
	fmt.Fprintf(&b, "时间窗：近 %s\n", d.Since)
	fmt.Fprintf(&b, "连接总数：%d，其中命中已知追踪器：%d\n", d.Trackers.Total, d.Trackers.Tracker)
	writeRanked(&b, "追踪器归属公司（连接数）", ownerRows(d.Trackers.Owners))
	writeRanked(&b, "出境节点 top（连接数）", aggRows(d.Nodes, "（未知）"))
	writeRanked(&b, "节点地区 top（连接数）", aggRows(d.Regions, "unknown"))
	return b.String()
}

// writeRanked 渲染一段排名（空则整段略去）；标签的空值回落已由 ownerRows/aggRows 处理。
func writeRanked(b *strings.Builder, title string, rows []labeledCount) {
	if len(rows) == 0 {
		return
	}
	fmt.Fprintf(b, "%s：\n", title)
	for _, r := range rows {
		fmt.Fprintf(b, "  - %s：%d\n", r.label, r.count)
	}
}

func ownerRows(owners []OwnerCount) []labeledCount {
	out := make([]labeledCount, len(owners))
	for i, o := range owners {
		label := o.Owner
		if label == "" {
			label = "（未知归属）"
		}
		out[i] = labeledCount{label, o.Count}
	}
	return out
}

func aggRows(rows []store.AggRow, emptyLabel string) []labeledCount {
	out := make([]labeledCount, len(rows))
	for i, r := range rows {
		label := r.Key
		if label == "" {
			label = emptyLabel
		}
		out[i] = labeledCount{label, r.Count}
	}
	return out
}
