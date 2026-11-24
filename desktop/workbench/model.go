package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Stringadmin/BoltCode/internal/agent"
	"github.com/Stringadmin/BoltCode/internal/command"
	"github.com/Stringadmin/BoltCode/internal/fio"
	"github.com/Stringadmin/BoltCode/internal/idgen"
	"github.com/Stringadmin/BoltCode/internal/keyring"
)

type Model struct {
	ID                 string                `json:"id"`
	ChannelID          string                `json:"channelId,omitempty"`
	Label              string                `json:"label"`
	Provider           string                `json:"provider,omitempty"`
	Name               string                `json:"name"`
	BaseURL            string                `json:"baseURL,omitempty"`
	Ready              bool                  `json:"ready"`
	HasKey             bool                  `json:"hasKey"`
	ReasoningEffort    agent.ReasoningEffort `json:"reasoningEffort,omitempty"`
	ReasoningSupported bool                  `json:"reasoningSupported"`
}

type Provider struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	BaseURL string `json:"baseURL"`
	Custom  bool   `json:"custom"`
}

func modelProviders() []Provider {
	providers := make([]Provider, 0, len(agent.KnownProviders)+3)
	for _, provider := range agent.KnownProviders {
		providers = append(providers, Provider{provider.ID, provider.Name, provider.BaseURL, provider.Custom})
		if provider.ID == "openai" {
			providers = append(providers, Provider{"responses", "OpenAI (Responses)", "https://api.openai.com/v1/responses", false})
		}
	}
	return append(providers,
		Provider{"groq", "Groq", "https://api.groq.com/openai/v1", false},
		Provider{"openrouter", "OpenRouter", "https://openrouter.ai/api/v1", false},
	)
}

type savedModel struct {
	Model
	Key           string `json:"key,omitempty"`
	ContextWindow int    `json:"contextWindow,omitempty"`
	fromCLI       bool
}

type modelStore struct {
	Active   string         `json:"active"`
	Profiles []savedModel   `json:"profiles"`
	Channels []savedChannel `json:"channels,omitempty"`
}

const legacyModelID = "default"

func (s *Service) modelPath() string {
	return filepath.Join(s.root, "model.json")
}

func (s *Service) loadModels() (modelStore, error) {
	data, err := os.ReadFile(s.modelPath())
	if errors.Is(err, os.ErrNotExist) {
		cfg, _ := (&command.App{}).LoadAgentConfig()
		store := modelStore{Profiles: []savedModel{}}
		if cfg != nil {
			if cfg.Provider == "" {
				cfg.Provider = "anthropic"
			}
			if cfg.Model == "" {
				cfg.Model = agent.DefaultModel
			}
			store.Active = legacyModelID
			store.Profiles = append(store.Profiles, savedModel{
				Model: modelSummary(legacyModelID, cfg.Model, *cfg),
				Key:   cfg.APIKey, ContextWindow: cfg.ContextWindow,
				fromCLI: true,
			})
		}
		store.migrateChannels()
		return store, nil
	}
	if err != nil {
		return modelStore{}, err
	}
	var store modelStore
	if err := json.Unmarshal(data, &store); err != nil {
		return modelStore{}, fmt.Errorf("读取模型配置：%w", err)
	}
	if store.Profiles != nil {
		store.migrateChannels()
		return store, nil
	}
	// model.json from earlier desktop versions held one profile.
	var record savedModel
	if err := json.Unmarshal(data, &record); err != nil {
		return modelStore{}, err
	}
	if record.Provider == "" || record.Name == "" {
		return modelStore{}, errors.New("无法读取已保存的模型配置")
	}
	record.ID, record.Label = legacyModelID, record.Name
	store = modelStore{Active: record.ID, Profiles: []savedModel{record}}
	store.migrateChannels()
	return store, nil
}

func (m *modelStore) profile(id string) *savedModel {
	for index := range m.Profiles {
		if m.Profiles[index].ID == id {
			return &m.Profiles[index]
		}
	}
	return nil
}

func (m *savedModel) config() (*agent.Config, error) {
	key, err := unwrapModelKey(m.Key, m.fromCLI)
	if err != nil {
		return nil, err
	}
	if key == "" && !isLoopbackEndpoint(m.BaseURL) {
		return nil, errors.New("请在模型设置中填写 API Key")
	}
	return &agent.Config{
		Provider: m.Provider, Model: m.Name, BaseURL: m.BaseURL, APIKey: key,
		ContextWindow: m.ContextWindow, ReasoningEffort: m.ReasoningEffort,
	}, nil
}

func (s *Service) agentConfig() (*agent.Config, error) {
	store, err := s.loadModels()
	if err != nil {
		return nil, err
	}
	if record := store.profile(store.Active); record != nil {
		record := store.hydrate(record)
		return record.config()
	}
	return nil, errors.New("请先配置并选择模型")
}

func (s *Service) modelState() (Model, []Model, []ModelChannel) {
	active := Model{Provider: "anthropic"}
	profiles := []Model{}
	channels := []ModelChannel{}
	store, err := s.loadModels()
	if err != nil {
		return active, profiles, channels
	}
	for _, channel := range store.Channels {
		channels = append(channels, channel.summary())
	}
	for i := range store.Profiles {
		record := store.hydrate(&store.Profiles[i])
		model := record.summary()
		profiles = append(profiles, model)
		if record.ID == store.Active {
			active = model
		}
	}
	return active, profiles, channels
}

func prepareModel(previous *savedModel, provider, name, baseURL, key string) (agent.Config, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return agent.Config{}, errors.New("请输入模型名称")
	}
	config, err := prepareConnection(previous, provider, baseURL, key)
	if err != nil {
		return agent.Config{}, err
	}
	config.Model = name
	if previous != nil && previous.Name == name && previous.Provider == config.Provider &&
		channelBaseURL(previous.Provider, previous.BaseURL) == config.BaseURL {
		config.ContextWindow = previous.ContextWindow
		config.ReasoningEffort = previous.ReasoningEffort
	}
	return config, nil
}

func prepareConnection(previous *savedModel, provider, baseURL, key string) (agent.Config, error) {
	provider = strings.TrimSpace(provider)
	baseURL, key = strings.TrimRight(strings.TrimSpace(baseURL), "/"), strings.TrimSpace(key)
	var baseDefault string
	known := false
	for _, candidate := range modelProviders() {
		if candidate.ID == provider {
			known = true
			baseDefault = candidate.BaseURL
			break
		}
	}
	if !known && (previous == nil || previous.Provider != provider) {
		return agent.Config{}, errors.New("请选择模型供应商")
	}
	if baseURL == "" {
		baseURL = baseDefault
		if provider == "custom" || !known {
			return agent.Config{}, errors.New("请填写此供应商的 API 地址")
		}
	}
	if baseURL != "" {
		parsed, err := url.Parse(baseURL)
		if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" ||
			(parsed.Scheme != "https" && parsed.Scheme != "http") {
			return agent.Config{}, errors.New("API 地址应为 HTTP 或 HTTPS URL")
		}
	}
	config := agent.Config{Provider: provider, BaseURL: baseURL, APIKey: key}
	if previous != nil && previous.Provider == provider {
		currentURL := strings.TrimRight(strings.TrimSpace(previous.BaseURL), "/")
		if currentURL == "" {
			currentURL = baseDefault
		}
		if currentURL == baseURL {
			if key == "" {
				current, err := previous.config()
				if err == nil {
					config.APIKey = current.APIKey
				}
			}
		}
	}
	if config.APIKey == "" && !isLoopbackEndpoint(baseURL) {
		return agent.Config{}, errors.New("请填写此配置及 API 地址对应的 API Key")
	}
	return config, nil
}

func isLoopbackEndpoint(address string) bool {
	endpoint, err := url.Parse(address)
	if err != nil {
		return false
	}
	host := endpoint.Hostname()
	return strings.EqualFold(host, "localhost") || net.ParseIP(host).IsLoopback()
}

func modelSummary(id, label string, config agent.Config) Model {
	return Model{ID: id, Label: label, Provider: config.Provider, Name: config.Model, BaseURL: config.BaseURL,
		Ready: true, HasKey: config.APIKey != "", ReasoningEffort: agent.NormalizeReasoningEffort(config.ReasoningEffort),
		ReasoningSupported: agent.SupportsReasoningSelection(config)}
}

func (s *Service) persistModels(store modelStore) error {
	for index := range store.Channels {
		channel := &store.Channels[index]
		if channel.fromCLI {
			wrapped, err := keyring.Wrap(channel.Key)
			if err != nil {
				return err
			}
			channel.Key, channel.fromCLI = wrapped, false
		}
	}
	data, err := json.Marshal(store)
	if err != nil {
		return err
	}
	return fio.WriteFileAtomic(s.modelPath(), data, 0o600)
}

// SaveModel preserves the single-model API by editing the active profile.
func (s *Service) SaveModel(provider, name, baseURL, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	store, err := s.loadModels()
	if err != nil {
		return err
	}
	_, err = s.saveModelProfile(store, store.Active, "", provider, name, baseURL, key)
	return err
}

func (s *Service) SaveModelProfile(id, label, provider, name, baseURL, key string) (Model, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	store, err := s.loadModels()
	if err != nil {
		return Model{}, err
	}
	return s.saveModelProfile(store, id, label, provider, name, baseURL, key)
}

func (s *Service) saveModelProfile(store modelStore, id, label, provider, name, baseURL, key string) (Model, error) {
	if s.active != nil || s.closed {
		return Model{}, errors.New("请先结束当前任务")
	}
	previous := store.profile(id)
	if id != "" && previous == nil {
		return Model{}, errors.New("找不到此模型配置")
	}
	original := store.hydrate(previous)
	config, err := prepareModel(original, provider, name, baseURL, key)
	if err != nil {
		return Model{}, err
	}
	if id == "" {
		id = idgen.New("m")
	}
	label = strings.Join(strings.Fields(label), " ")
	if label == "" {
		label = config.Model
	}
	model := modelSummary(id, label, config)
	channel, err := store.getOrAddChannel(config, original)
	if err != nil {
		return Model{}, err
	}
	model.ChannelID = channel.ID
	record := storedModel(model, config.ContextWindow)
	if previous == nil {
		store.Profiles = append(store.Profiles, record)
	} else {
		*previous = record
	}
	if store.Active == "" {
		store.Active = id
	}
	return model, s.persistModels(store)
}

func (s *Service) SelectModel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil || s.closed {
		return errors.New("请先结束当前任务")
	}
	store, err := s.loadModels()
	if err != nil {
		return err
	}
	record := store.hydrate(store.profile(id))
	if record == nil {
		return errors.New("找不到此模型配置")
	}
	if _, err := record.config(); err != nil {
		return err
	}
	if id == store.Active {
		return nil
	}
	store.Active = id
	return s.persistModels(store)
}

func (s *Service) SetModelReasoningEffort(id, raw string) error {
	effort, err := agent.ParseReasoningEffort(raw)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil || s.checking != nil || s.closed {
		return errors.New("请先结束当前任务或验证")
	}
	store, err := s.loadModels()
	if err != nil {
		return err
	}
	profile := store.profile(id)
	if profile == nil || id != store.Active {
		return errors.New("当前模型已更换，请重新选择思维强度")
	}
	hydrated := store.hydrate(profile)
	config, err := hydrated.config()
	if err != nil {
		return err
	}
	if effort != agent.ReasoningAuto && !agent.SupportsReasoningSelection(*config) {
		return errors.New("此模型不提供可调的思维强度")
	}
	profile.ReasoningEffort = effort
	return s.persistModels(store)
}

// TestModel tests the active profile's form values without saving them.
func (s *Service) TestModel(provider, name, baseURL, key string) (int64, error) {
	return s.testModel("", true, provider, name, baseURL, key)
}

func (s *Service) TestModelProfile(id, provider, name, baseURL, key string) (int64, error) {
	return s.testModel(id, false, provider, name, baseURL, key)
}

func (s *Service) testModel(id string, active bool, provider, name, baseURL, key string) (int64, error) {
	s.mu.Lock()
	store, err := s.loadModels()
	if active {
		id = store.Active
	}
	previous := store.hydrate(store.profile(id))
	s.mu.Unlock()
	if err != nil {
		return 0, err
	}
	if id != "" && previous == nil {
		return 0, errors.New("找不到此模型配置")
	}
	config, err := prepareModel(previous, provider, name, baseURL, key)
	if err != nil {
		return 0, err
	}
	return checkModelConnection(config)
}

func checkModelConnection(config agent.Config) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	start := time.Now()
	if err := agent.CheckConnection(ctx, config); err != nil {
		if ctx.Err() != nil {
			return 0, errors.New("模型连接超时，请检查网络和 API 地址")
		}
		return 0, fmt.Errorf("连接失败：%s", modelError(err, config.APIKey))
	}
	return time.Since(start).Milliseconds(), nil
}

func modelError(err error, key string) string {
	detail := redact(err.Error(), key)
	switch {
	case strings.Contains(detail, "API 401") || strings.Contains(detail, "API 403"):
		return "模型服务拒绝认证，请检查 API Key 和访问权限。\n" + detail
	case strings.Contains(detail, "API 404"):
		return "找不到模型或接口，请检查模型 ID 和 API 地址。\n" + detail
	case strings.Contains(detail, "API 429"):
		return "模型服务限制了请求，请检查用量和并发限制。\n" + detail
	case strings.Contains(detail, "API 400") && (strings.Contains(detail, "reasoning_effort") ||
		strings.Contains(detail, "reasoning.effort") || strings.Contains(detail, "output_config.effort")):
		return "模型服务拒绝了当前思维强度，可以切回「默认」后重试。\n" + detail
	default:
		return detail
	}
}
