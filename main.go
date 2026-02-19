package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/slack-go/slack"
)

type Service struct {
	Name string `json:"name"`
	URL string `json:"url"`
}

type Config struct {
	IntervalSeconds int `json:"interval_seconds"`
	TimeoutMs int `json:"timeout_ms"`
	Concurrency int `json:"concurrency"`
	Services []Service `json:"services"`
}

type CheckResult struct {
	Service    Service
    Up         bool
    StatusCode int
    Latency    time.Duration
    Error      string
}

type ServiceState struct {
    IsDown    bool
    FailCount int
    DownSince time.Time
}

type Transition struct {
    ServiceName string
    Type        string
    Error       string
}

const failThreshold = 4

func formatDuration(d time.Duration) string {
    if d < time.Minute {
        return fmt.Sprintf("%ds", int(d.Seconds()))
    }
    if d < time.Hour {
        return fmt.Sprintf("%dm", int(d.Minutes()))
    }
    hours := int(d.Hours())
    minutes := int(d.Minutes()) % 60
    if minutes == 0 {
        return fmt.Sprintf("%dh", hours)
    }
    return fmt.Sprintf("%dh%dm", hours, minutes)
}

func loadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read file: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse json: %w", err)
	}

	if cfg.IntervalSeconds <= 0 {
		return Config{}, fmt.Errorf("interval_seconds must be greater than 0")
	}

	if cfg.TimeoutMs <= 0 {
		return Config{}, fmt.Errorf("timeout_ms must be greater than 0")
	}

	if cfg.Concurrency <= 0 {
		return Config{}, fmt.Errorf("concurrency must be greater than 0")
	}

	if len(cfg.Services) == 0 {
		return Config{}, fmt.Errorf("no services defined")
	}

	return cfg, nil
}

func checkService(ctx context.Context, client *http.Client, svc Service) CheckResult {
    start := time.Now()

    req, err := http.NewRequestWithContext(ctx, http.MethodGet, svc.URL, nil)
    if err != nil {
        return CheckResult{
            Service: svc,
            Up:      false,
            Latency: time.Since(start),
            Error:   "invalid url",
        }
    }

    resp, err := client.Do(req)
    latency := time.Since(start)

    if err != nil {
        return CheckResult{
            Service: svc,
            Up:      false,
            Latency: latency,
            Error:   "request failed",
        }
    }

    defer resp.Body.Close()

    up := resp.StatusCode >= 200 && resp.StatusCode < 300
    result := CheckResult{
        Service:    svc,
        Up:         up,
        StatusCode: resp.StatusCode,
        Latency:    latency,
    }

    if !up {
        result.Error = fmt.Sprintf("http_%d", resp.StatusCode)
    }

    return result
}

func overallEmoji(results []CheckResult) string {
    for _, r := range results {
        if !r.Up {
            return "🔴"
        }
    }

    return "🟢"
}

func countStatus(results []CheckResult) (healthy int, down int) {
    for _, r := range results {
        if r.Up {
            healthy++
        } else {
            down++
        }
    }

    return
}

func loadBoardTS(path string) string {
    data, err := os.ReadFile(path)
    if err != nil {
        return ""
    }

    return strings.TrimSpace(string(data))
}

func saveBoardTS(path string, ts string) error {
    return os.WriteFile(path, []byte(ts), 0600)
}

func upsertBoard(api *slack.Client, channelID string, tsPath string, blocks []slack.Block) error {
    ts := loadBoardTS(tsPath)

    if ts == "" {
        _, newTS, err := api.PostMessage(channelID, slack.MsgOptionBlocks(blocks...))
        if err != nil {
            return fmt.Errorf("post message: %w", err)
        }
        return saveBoardTS(tsPath, newTS)
    }

    _, _, _, err := api.UpdateMessage(channelID, ts, slack.MsgOptionBlocks(blocks...))
    if err != nil {
        _, newTS, err := api.PostMessage(channelID, slack.MsgOptionBlocks(blocks...))
        if err != nil {
            return fmt.Errorf("post message: %w", err)
        }
        return saveBoardTS(tsPath, newTS)
    }

    return nil
}

func postThreadAlert(api *slack.Client, channelID string, tsPath string, message string) error {
    ts := loadBoardTS(tsPath)
    if ts == "" {
        return fmt.Errorf("no board message to reply to")
    }

    _, _, err := api.PostMessage(
        channelID,
        slack.MsgOptionText(message, false),
        slack.MsgOptionTS(ts),
    )
    return err
}

func detectTransitions(results []CheckResult, states map[string]*ServiceState) []Transition {
    var transitions []Transition

    for _, r := range results {
        name := r.Service.Name
        state, exists := states[name]
        if !exists {
            state = &ServiceState{}
            states[name] = state
        }

        if r.Up {
            if state.IsDown {
                transitions = append(transitions, Transition{
                    ServiceName: name,
                    Type:        "up",
                })
                state.IsDown = false
            }
            state.FailCount = 0
        } else {
            state.FailCount++
            if !state.IsDown && state.FailCount >= failThreshold {
                transitions = append(transitions, Transition{
                    ServiceName: name,
                    Type:        "down",
                    Error:       r.Error,
                })
                state.IsDown = true
                state.DownSince = time.Now()
            }
        }
    }

    return transitions
}

func sendAlerts(api *slack.Client, channelID string, tsPath string, transitions []Transition) {
    for _, t := range transitions {
        var msg string
        if t.Type == "down" {
            msg = fmt.Sprintf("🔴 *%s* is DOWN: `%s` <!here>", t.ServiceName, t.Error)
        } else {
            msg = fmt.Sprintf("🟢 *%s* is back UP", t.ServiceName)
        }

        if err := postThreadAlert(api, channelID, tsPath, msg); err != nil {
            fmt.Fprintf(os.Stderr, "failed to post alert: %v\n", err)
        }
    }
}

func renderBoard(results []CheckResult, states map[string]*ServiceState) []slack.Block {
    var blocks []slack.Block

    updateText := fmt.Sprintf("Updated: %s", time.Now().Format("2006-01-02 15:04:05"))
    blocks = append(blocks, slack.NewContextBlock("",
        slack.NewTextBlockObject(slack.MarkdownType, updateText, false, false),
    ))

    blocks = append(blocks, slack.NewDividerBlock())

    for _, r := range results {
        var emoji, statusText string
        if r.Up {
            emoji = "🟢"
            statusText = fmt.Sprintf("`%dms`", r.Latency.Milliseconds())
        } else {
            emoji = "🔴"
            state := states[r.Service.Name]
            if state != nil && !state.DownSince.IsZero() {
                downtime := formatDuration(time.Since(state.DownSince))
                statusText = fmt.Sprintf("`%s (%s)`", r.Error, downtime)
            } else {
                statusText = fmt.Sprintf("`%s`", r.Error)
            }
        }

        text := fmt.Sprintf("%s  *%s:* %s", emoji, r.Service.Name, statusText)
        blocks = append(blocks, slack.NewSectionBlock(
            slack.NewTextBlockObject(slack.MarkdownType, text, false, false),
            nil, nil,
        ))
    }

    blocks = append(blocks, slack.NewDividerBlock())

    healthy, down := countStatus(results)
    footerText := fmt.Sprintf("%d healthy  •  %d down", healthy, down)
    blocks = append(blocks, slack.NewContextBlock("",
        slack.NewTextBlockObject(slack.MarkdownType, footerText, false, false),
    ))

    return blocks
}

func runCycle(ctx context.Context, api *slack.Client, client *http.Client, cfg Config, channelID string, states map[string]*ServiceState) error {
	var results []CheckResult
	for _, svc := range cfg.Services {
		result := checkService(ctx, client, svc)
		results = append(results, result)
		fmt.Printf("%s: up=%v, latency=%v\n", result.Service.Name, result.Up, result.Latency)
	}

	transitions := detectTransitions(results, states)

	blocks := renderBoard(results, states)

	if err := upsertBoard(api, channelID, ".board_ts", blocks); err != nil {
		return fmt.Errorf("upsert board: %w", err)
	}

	sendAlerts(api, channelID, ".board_ts", transitions)

	fmt.Println("Board updated successfully")
	return nil
}

func run() error {
	token := os.Getenv("SLACK_BOT_TOKEN")
	if token == "" {
		return fmt.Errorf("SLACK_BOT_TOKEN is not set")
	}

	channelID := os.Getenv("SLACK_CHANNEL_ID")
	if channelID == "" {
		return fmt.Errorf("SLACK_CHANNEL_ID is not set")
	}

	cfg, err := loadConfig("services.json")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	fmt.Printf("Loaded %d services, checking every %ds\n", len(cfg.Services), cfg.IntervalSeconds)

	api := slack.New(token)
	client := &http.Client{Timeout: time.Duration(cfg.TimeoutMs) * time.Millisecond}
	states := make(map[string]*ServiceState)

	ctx := context.Background()
	if err := runCycle(ctx, api, client, cfg, channelID, states); err != nil {
		fmt.Fprintf(os.Stderr, "cycle error: %v\n", err)
	}

	ticker := time.NewTicker(time.Duration(cfg.IntervalSeconds) * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if err := runCycle(ctx, api, client, cfg, channelID, states); err != nil {
			fmt.Fprintf(os.Stderr, "cycle error: %v\n", err)
		}
	}

	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
