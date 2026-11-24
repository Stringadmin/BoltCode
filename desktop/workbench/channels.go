package workbench

import (
	"errors"
	"strings"

	"github.com/Stringadmin/BoltCode/internal/agent"
	"github.com/Stringadmin/BoltCode/internal/idgen"
	"github.com/Stringadmin/BoltCode/internal/keyring"
)

type ModelChannel struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Provider string `json:"provider"`
	BaseURL  string `json:"baseURL"`
	HasKey   bool   `json:"hasKey"`
}

type savedChannel struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Provider string `json:"provider"`
	BaseURL  string `json:"baseURL"`
	Key      string `json:"key,omitempty"`
	fromCLI  bool
}

func unwrapModelKey(value string, plain bool) (string, error) {
	if plain {
		return value, nil
	}
	key, err := keyring.Unwrap(value)
	if err != nil {
		return "", errors.New("无法读取此配置的 API Key，请重新填写")
	}
	return key, nil
}

func channelBaseURL(provider, address string) string {
	if clean := strings.TrimRight(strings.TrimSpace(address), "/"); clean != "" {
		return clean
	}
	for _, entry := range modelProviders() {
		if entry.ID == provider {
			return entry.BaseURL
		}
	}
	return ""
}

func (c savedChannel) summary() ModelChannel {
	key, err := unwrapModelKey(c.Key, c.fromCLI)
	return ModelChannel{ID: c.ID, Label: c.Label, Provider: c.Provider, BaseURL: c.BaseURL, HasKey: err == nil && key != ""}
}

func (m *modelStore) channel(id string) *savedChannel {
	for i := range m.Channels {
		if m.Channels[i].ID == id {
			return &m.Channels[i]
		}
	}
	return nil
}

func (m *modelStore) hydrate(record *savedModel) *savedModel {
	if record == nil {
		return nil
	}
	model := *record
	if channel := m.channel(model.ChannelID); channel != nil {
		model.Provider, model.BaseURL = channel.Provider, channel.BaseURL
		model.Key, model.fromCLI = channel.Key, channel.fromCLI
	}
	return &model
}

func (m *savedModel) summary() Model {
	model := m.Model
	cfg, err := m.config()
	model.Ready = err == nil
	model.HasKey = cfg != nil && cfg.APIKey != ""
	model.ReasoningEffort = agent.NormalizeReasoningEffort(m.ReasoningEffort)
	model.ReasoningSupported = agent.SupportsReasoningSelection(agent.Config{Provider: m.Provider, Model: m.Name, BaseURL: m.BaseURL})
	return model
}

func storedModel(model Model, contextWindow int) savedModel {
	model.Provider, model.BaseURL = "", ""
	model.Ready, model.HasKey, model.ReasoningSupported = false, false, false
	return savedModel{Model: model, ContextWindow: contextWindow}
}

func (c savedChannel) matches(provider, address, key string) bool {
	if c.Provider != provider || c.BaseURL != address {
		return false
	}
	actual, err := unwrapModelKey(c.Key, c.fromCLI)
	return err == nil && actual == key
}

func (m *modelStore) matchingChannel(provider, address, key string) *savedChannel {
	for i := range m.Channels {
		if m.Channels[i].matches(provider, address, key) {
			return &m.Channels[i]
		}
	}
	return nil
}

func (m *modelStore) migrateChannels() {
	for i := range m.Profiles {
		record := &m.Profiles[i]
		if record.ChannelID != "" || record.Provider == "" {
			continue
		}
		address := channelBaseURL(record.Provider, record.BaseURL)
		key, err := unwrapModelKey(record.Key, record.fromCLI)
		var channel *savedChannel
		if err == nil {
			channel = m.matchingChannel(record.Provider, address, key)
		}
		if channel == nil {
			// The ID stays stable across read-only openings of a legacy file.
			m.Channels = append(m.Channels, savedChannel{
				ID: "legacy-" + record.ID, Label: record.Label, Provider: record.Provider,
				BaseURL: address, Key: record.Key, fromCLI: record.fromCLI,
			})
			channel = &m.Channels[len(m.Channels)-1]
		}
		model := record.Model
		model.ChannelID = channel.ID
		*record = storedModel(model, record.ContextWindow)
	}
}

func (m *modelStore) getOrAddChannel(config agent.Config, previous *savedModel) (*savedChannel, error) {
	if previous != nil {
		if channel := m.channel(previous.ChannelID); channel != nil && channel.matches(config.Provider, config.BaseURL, config.APIKey) {
			return channel, nil
		}
	}
	if channel := m.matchingChannel(config.Provider, config.BaseURL, config.APIKey); channel != nil {
		return channel, nil
	}
	key, err := keyring.Wrap(config.APIKey)
	if err != nil {
		return nil, err
	}
	m.Channels = append(m.Channels, savedChannel{
		ID: idgen.New("ch"), Label: config.Model, Provider: config.Provider, BaseURL: config.BaseURL, Key: key,
	})
	return &m.Channels[len(m.Channels)-1], nil
}

func (s *Service) SaveModelChannel(id, label, provider, baseURL, key string) (ModelChannel, error) {
	label = strings.Join(strings.Fields(label), " ")
	if label == "" {
		return ModelChannel{}, errors.New("请输入渠道名称")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil || s.closed {
		return ModelChannel{}, errors.New("请先结束当前任务")
	}
	store, err := s.loadModels()
	if err != nil {
		return ModelChannel{}, err
	}
	channel := store.channel(id)
	var previous *savedModel
	if id != "" {
		if channel == nil {
			return ModelChannel{}, errors.New("找不到此渠道")
		}
		previous = &savedModel{Model: Model{Provider: channel.Provider, BaseURL: channel.BaseURL}, Key: channel.Key, fromCLI: channel.fromCLI}
	}
	config, err := prepareConnection(previous, provider, baseURL, key)
	if err != nil {
		return ModelChannel{}, err
	}
	wrapped, err := keyring.Wrap(config.APIKey)
	if err != nil {
		return ModelChannel{}, err
	}
	if id == "" {
		id = idgen.New("ch")
	}
	record := savedChannel{ID: id, Label: label, Provider: config.Provider, BaseURL: config.BaseURL, Key: wrapped}
	if channel == nil {
		store.Channels = append(store.Channels, record)
	} else {
		*channel = record
	}
	return record.summary(), s.persistModels(store)
}

func (s *Service) SaveChannelModel(channelID, id, label, name, rawEffort string) (Model, error) {
	name, label = strings.TrimSpace(name), strings.Join(strings.Fields(label), " ")
	if name == "" {
		return Model{}, errors.New("请输入模型 ID")
	}
	if label == "" {
		label = name
	}
	effort, err := agent.ParseReasoningEffort(rawEffort)
	if err != nil {
		return Model{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil || s.closed {
		return Model{}, errors.New("请先结束当前任务")
	}
	store, err := s.loadModels()
	if err != nil {
		return Model{}, err
	}
	channel := store.channel(channelID)
	if channel == nil {
		return Model{}, errors.New("请先保存渠道")
	}
	previous := store.profile(id)
	if id != "" && (previous == nil || previous.ChannelID != channelID) {
		return Model{}, errors.New("找不到此渠道下的模型")
	}
	if id == "" {
		id = idgen.New("m")
	}
	contextWindow := 0
	if previous != nil && previous.Name == name {
		contextWindow = previous.ContextWindow
	}
	record := storedModel(Model{ID: id, ChannelID: channelID, Label: label, Name: name, ReasoningEffort: effort}, contextWindow)
	hydrated := store.hydrate(&record)
	cfg, err := hydrated.config()
	if err != nil {
		return Model{}, err
	}
	if effort != agent.ReasoningAuto && !agent.SupportsReasoningSelection(*cfg) {
		return Model{}, errors.New("此模型不提供可调的思维强度，请选「默认」")
	}
	if previous == nil {
		store.Profiles = append(store.Profiles, record)
	} else {
		*previous = record
	}
	if store.Active == "" {
		store.Active = id
	}
	return hydrated.summary(), s.persistModels(store)
}

func (s *Service) TestChannelModel(channelID, name string) (int64, error) {
	s.mu.Lock()
	store, err := s.loadModels()
	s.mu.Unlock()
	if err != nil {
		return 0, err
	}
	if name = strings.TrimSpace(name); name == "" {
		return 0, errors.New("请输入模型 ID")
	}
	if store.channel(channelID) == nil {
		return 0, errors.New("请先保存渠道")
	}
	cfg, err := store.hydrate(&savedModel{Model: Model{ChannelID: channelID, Name: name}}).config()
	if err != nil {
		return 0, err
	}
	return checkModelConnection(*cfg)
}
