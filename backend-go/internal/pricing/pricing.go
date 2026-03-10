package pricing

import (
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// ModelPricing 单个模型的定价配置
// 价格单位：美元/百万tokens
type ModelPricing struct {
	InputPrice         float64  `json:"inputPrice"`               // 输入 token 价格 ($/1M tokens)
	OutputPrice        float64  `json:"outputPrice"`              // 输出 token 价格 ($/1M tokens)
	CacheCreationPrice *float64 `json:"cacheCreationPrice"`       // 缓存创建价格 ($/1M tokens)，nil 时使用 inputPrice * 1.25
	CacheReadPrice     *float64 `json:"cacheReadPrice"`           // 缓存读取价格 ($/1M tokens)，nil 时使用 inputPrice * 0.1
	Description        string   `json:"description,omitempty"`    // 模型描述
	ExportToModels     *bool    `json:"exportToModels,omitempty"` // 是否导出到 /v1/models API，nil 或 true 时导出
}

// PricingConfig 定价配置
type PricingConfig struct {
	Models   map[string]ModelPricing `json:"models"`   // 模型定价表
	Currency string                  `json:"currency"` // 货币单位，默认 USD
}

// PricingManager 定价管理器
type PricingManager struct {
	mu         sync.RWMutex
	config     PricingConfig
	configFile string
	watcher    *fsnotify.Watcher
	dbStorage  *DBPricingStorage // Database storage adapter (nil if using JSON-only mode)
}

var (
	globalManager *PricingManager
	once          sync.Once
)

// GetManager 获取全局定价管理器
func GetManager() *PricingManager {
	return globalManager
}

// InitManager 初始化全局定价管理器
func InitManager(configFile string) (*PricingManager, error) {
	var initErr error
	once.Do(func() {
		pm := &PricingManager{
			configFile: configFile,
		}

		if err := pm.loadConfig(); err != nil {
			// 如果配置文件不存在，使用默认配置
			log.Printf("⚠️ 定价配置文件不存在，使用默认配置: %v", err)
			pm.config = getDefaultPricingConfig()
		}

		// 启动文件监控
		if err := pm.startWatcher(); err != nil {
			log.Printf("⚠️ 无法启动定价配置监控: %v", err)
		}

		globalManager = pm
	})
	return globalManager, initErr
}

// loadConfig 加载配置文件
func (pm *PricingManager) loadConfig() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	data, err := os.ReadFile(pm.configFile)
	if err != nil {
		return err
	}

	var config PricingConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return err
	}

	pm.config = config
	log.Printf("✅ 定价配置已加载: %d 个模型", len(config.Models))
	return nil
}

// startWatcher 启动文件监控
func (pm *PricingManager) startWatcher() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	pm.watcher = watcher

	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					log.Printf("📝 定价配置文件已更新，重新加载...")
					if err := pm.loadConfig(); err != nil {
						log.Printf("⚠️ 重新加载定价配置失败: %v", err)
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("⚠️ 定价配置监控错误: %v", err)
			}
		}
	}()

	return watcher.Add(pm.configFile)
}

// CostBreakdown 成本明细
type CostBreakdown struct {
	InputCost         float64 `json:"inputCost"`         // 输入成本
	OutputCost        float64 `json:"outputCost"`        // 输出成本
	CacheCreationCost float64 `json:"cacheCreationCost"` // 缓存创建成本
	CacheReadCost     float64 `json:"cacheReadCost"`     // 缓存读取成本
	TotalCost         float64 `json:"totalCost"`         // 总成本
}

// PriceMultipliers 价格乘数（从 config 包传入）
type PriceMultipliers struct {
	InputMultiplier         float64
	OutputMultiplier        float64
	CacheCreationMultiplier float64
	CacheReadMultiplier     float64
}

// ApplyFastModeMultiplier applies a 2x cost multiplier for Codex fast mode
// (service_tier: "priority"). When isFastMode is true, all multiplier fields
// are doubled. This composes with any existing channel-level multipliers.
// Returns the original pointer unchanged when isFastMode is false.
func ApplyFastModeMultiplier(multipliers *PriceMultipliers, isFastMode bool) *PriceMultipliers {
	if !isFastMode {
		return multipliers
	}
	if multipliers == nil {
		return &PriceMultipliers{
			InputMultiplier:         2.0,
			OutputMultiplier:        2.0,
			CacheCreationMultiplier: 2.0,
			CacheReadMultiplier:     2.0,
		}
	}
	return &PriceMultipliers{
		InputMultiplier:         getEffectiveMultiplier(multipliers.InputMultiplier) * 2.0,
		OutputMultiplier:        getEffectiveMultiplier(multipliers.OutputMultiplier) * 2.0,
		CacheCreationMultiplier: getEffectiveMultiplier(multipliers.CacheCreationMultiplier) * 2.0,
		CacheReadMultiplier:     getEffectiveMultiplier(multipliers.CacheReadMultiplier) * 2.0,
	}
}

// getEffectiveMultiplier 获取有效乘数（0 时返回 1.0）
func getEffectiveMultiplier(m float64) float64 {
	if m == 0 {
		return 1.0
	}
	return m
}

// CalculateCost 计算请求成本
// 返回值单位：美元
func (pm *PricingManager) CalculateCost(model string, inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens int) float64 {
	breakdown := pm.CalculateCostWithBreakdown(model, inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens, nil)
	return breakdown.TotalCost
}

// CalculateCostWithBreakdown 计算请求成本并返回明细
// multipliers 为 nil 时使用默认乘数 1.0
func (pm *PricingManager) CalculateCostWithBreakdown(model string, inputTokens, outputTokens, cacheCreationTokens, cacheReadTokens int, multipliers *PriceMultipliers) CostBreakdown {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := CostBreakdown{}

	pricing := pm.findPricing(model)
	if pricing == nil {
		return result
	}

	// 获取乘数（默认为 1.0）
	inputMult := 1.0
	outputMult := 1.0
	cacheCreationMult := 1.0
	cacheReadMult := 1.0
	if multipliers != nil {
		inputMult = getEffectiveMultiplier(multipliers.InputMultiplier)
		outputMult = getEffectiveMultiplier(multipliers.OutputMultiplier)
		cacheCreationMult = getEffectiveMultiplier(multipliers.CacheCreationMultiplier)
		cacheReadMult = getEffectiveMultiplier(multipliers.CacheReadMultiplier)
	}

	// 价格单位是 $/1M tokens，所以需要除以 1,000,000
	result.InputCost = float64(inputTokens) * pricing.InputPrice * inputMult / 1_000_000
	result.OutputCost = float64(outputTokens) * pricing.OutputPrice * outputMult / 1_000_000

	// 缓存价格，如果未配置（nil）则使用默认比例，显式设置为 0 表示免费
	var cacheCreationPrice float64
	if pricing.CacheCreationPrice == nil {
		cacheCreationPrice = pricing.InputPrice * 1.25 // 默认缓存创建价格是输入价格的 1.25 倍
	} else {
		cacheCreationPrice = *pricing.CacheCreationPrice
	}
	var cacheReadPrice float64
	if pricing.CacheReadPrice == nil {
		cacheReadPrice = pricing.InputPrice * 0.1 // 默认缓存读取价格是输入价格的 0.1 倍
	} else {
		cacheReadPrice = *pricing.CacheReadPrice
	}

	result.CacheCreationCost = float64(cacheCreationTokens) * cacheCreationPrice * cacheCreationMult / 1_000_000
	result.CacheReadCost = float64(cacheReadTokens) * cacheReadPrice * cacheReadMult / 1_000_000

	result.TotalCost = result.InputCost + result.OutputCost + result.CacheCreationCost + result.CacheReadCost

	return result
}

// HasPricing 检查模型是否有定价配置
func (pm *PricingManager) HasPricing(model string) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.findPricing(model) != nil
}

// findPricing 查找模型定价（支持前缀匹配）
func (pm *PricingManager) findPricing(model string) *ModelPricing {
	// 精确匹配
	if pricing, ok := pm.config.Models[model]; ok {
		return &pricing
	}

	// 前缀匹配（例如 claude-3-5-sonnet-20241022 匹配 claude-3-5-sonnet）
	for pattern, pricing := range pm.config.Models {
		if strings.HasPrefix(model, pattern) {
			p := pricing // 创建副本
			return &p
		}
	}

	// 尝试匹配模型系列（例如 claude-3-opus 匹配任何 claude-3-opus-* 变体）
	modelLower := strings.ToLower(model)
	for pattern, pricing := range pm.config.Models {
		patternLower := strings.ToLower(pattern)
		if strings.HasPrefix(modelLower, patternLower) {
			p := pricing
			return &p
		}
	}

	return nil
}

// GetConfig 获取当前配置（用于 API 展示）
func (pm *PricingManager) GetConfig() PricingConfig {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.config
}

// GetExportableModels 获取可导出到 /v1/models API 的模型列表
func (pm *PricingManager) GetExportableModels() map[string]ModelPricing {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	result := make(map[string]ModelPricing)
	for modelID, pricing := range pm.config.Models {
		// nil 或 true 时导出
		if pricing.ExportToModels == nil || *pricing.ExportToModels {
			result[modelID] = pricing
		}
	}
	return result
}

// SetDBStorage sets the database storage adapter for write-through caching
func (pm *PricingManager) SetDBStorage(dbStorage *DBPricingStorage) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.dbStorage = dbStorage
	// Disable file watcher when using database storage (polling handles sync)
	if pm.watcher != nil {
		pm.watcher.Close()
		pm.watcher = nil
	}
}

// UpdateConfigFromDB replaces the in-memory config with data loaded from the database.
// This is called during startup to ensure the manager has the latest DB state.
func (pm *PricingManager) UpdateConfigFromDB(config PricingConfig) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.config = config
}

// UpdateConfig 更新配置
func (pm *PricingManager) UpdateConfig(config PricingConfig) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// When using database storage, write to DB instead of JSON file
	if pm.dbStorage != nil {
		pm.config = config

		// Write to database asynchronously (non-blocking)
		configCopy := config
		go func() {
			if err := pm.dbStorage.SaveConfigToDB(configCopy); err != nil {
				log.Printf("⚠️ Failed to sync pricing to database: %v", err)
			}
		}()

		return nil
	}

	// JSON-only mode: write to file
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(pm.configFile, data, 0644); err != nil {
		return err
	}

	pm.config = config
	return nil
}

// Close 关闭管理器
func (pm *PricingManager) Close() error {
	if pm.watcher != nil {
		return pm.watcher.Close()
	}
	return nil
}

// GetDefaultPricingConfig 获取默认定价配置 (exported for handlers)
func GetDefaultPricingConfig() PricingConfig {
	return getDefaultPricingConfig()
}

// floatPtr 辅助函数，用于创建 float64 指针
func floatPtr(v float64) *float64 {
	return &v
}

// getDefaultPricingConfig 获取默认定价配置
func getDefaultPricingConfig() PricingConfig {
	return PricingConfig{
		Currency: "USD",
		Models: map[string]ModelPricing{
			// Claude 4.5 系列 (使用前缀匹配)
			"claude-opus-4-5": {
				InputPrice:         5.0,
				OutputPrice:        25.0,
				CacheCreationPrice: floatPtr(6.25),
				CacheReadPrice:     floatPtr(0.50),
				Description:        "Claude Opus 4.5",
			},
			"claude-sonnet-4-5": {
				InputPrice:         3.0,
				OutputPrice:        15.0,
				CacheCreationPrice: floatPtr(3.75),
				CacheReadPrice:     floatPtr(0.30),
				Description:        "Claude Sonnet 4.5",
			},
			"claude-haiku-4-5": {
				InputPrice:         1.0,
				OutputPrice:        5.0,
				CacheCreationPrice: floatPtr(1.25),
				CacheReadPrice:     floatPtr(0.10),
				Description:        "Claude Haiku 4.5",
			},
			// GPT-5.1 系列
			"gpt-5.1": {
				InputPrice:  1.25,
				OutputPrice: 10.0,
				Description: "GPT-5.1",
			},
			"gpt-5.1-codex": {
				InputPrice:  1.25,
				OutputPrice: 10.0,
				Description: "GPT-5.1 Codex",
			},
		},
	}
}
