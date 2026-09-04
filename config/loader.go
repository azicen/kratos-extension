package config

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	kratosconfig "github.com/go-kratos/kratos/v3/config"
	"github.com/go-kratos/kratos/v3/config/file"
)

var baseConfigNames = []string{"config.toml", "config.yaml", "config.yml"}

// BuildSources 准备配置目录，并按基础配置 + conf.d 的顺序返回配置源
//
// 加载顺序：
//  1. config.toml / config.yaml / config.yml
//  2. conf.d 目录下按文件名字典序升序排列的配置文件
//
// 后加载的配置会覆盖先加载的配置，行为类似常见 Linux 服务配置覆盖逻辑
func BuildSources(confPath string, defaultConfigFS embed.FS) ([]kratosconfig.Source, error) {
	configDir, err := ensureConfigDir(confPath)
	if err != nil {
		return nil, err
	}

	baseFiles, err := ensureBaseConfig(configDir, defaultConfigFS)
	if err != nil {
		return nil, err
	}

	confDDir, err := ensureConfDDir(configDir)
	if err != nil {
		return nil, err
	}

	confDFiles, err := listConfigFiles(confDDir)
	if err != nil {
		return nil, err
	}

	paths := append(baseFiles, confDFiles...)
	sources := make([]kratosconfig.Source, 0, len(paths))
	for _, p := range paths {
		sources = append(sources, file.NewSource(p))
	}

	return sources, nil
}

// ensureConfigDir 规范化配置路径，并确保配置目录存在
//
// 当传入的是具体配置文件路径时，会自动取其所在目录作为配置目录
func ensureConfigDir(confPath string) (string, error) {
	if confPath == "" {
		return "", fmt.Errorf("config path is empty")
	}

	cleanPath := filepath.Clean(confPath)
	if isConfigFilePath(cleanPath) {
		cleanPath = filepath.Dir(cleanPath)
	}

	if err := os.MkdirAll(cleanPath, 0o755); err != nil {
		return "", fmt.Errorf("create config dir failed: %w", err)
	}

	return cleanPath, nil
}

// ensureBaseConfig 确保基础配置文件存在，并返回可用的基础配置文件列表
//
// 若目录下不存在约定的基础配置文件，则会写入内嵌的默认 config.toml
func ensureBaseConfig(configDir string, defaultConfigFS embed.FS) ([]string, error) {
	baseFiles, err := listExistingBaseConfigs(configDir)
	if err != nil {
		return nil, err
	}

	if len(baseFiles) > 0 {
		return baseFiles, nil
	}

	target := filepath.Join(configDir, "config.toml")
	content, err := defaultConfigFS.ReadFile("config.toml")
	if err != nil {
		return nil, fmt.Errorf("read embedded default config failed: %w", err)
	}
	if err := os.WriteFile(target, content, 0o644); err != nil {
		return nil, fmt.Errorf("write default config failed: %w", err)
	}

	return []string{target}, nil
}

// listExistingBaseConfigs 扫描配置目录中的基础配置文件，并按预定义顺序返回
func listExistingBaseConfigs(configDir string) ([]string, error) {
	files := make([]string, 0, len(baseConfigNames))
	for _, name := range baseConfigNames {
		path := filepath.Join(configDir, name)
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("stat config file failed: %w", err)
		}
		if info.IsDir() {
			continue
		}
		files = append(files, path)
	}
	return files, nil
}

// ensureConfDDir 确保 conf.d 覆盖配置目录存在，并返回其路径
func ensureConfDDir(configDir string) (string, error) {
	confDDir := filepath.Join(configDir, "conf.d")
	if err := os.MkdirAll(confDDir, 0o755); err != nil {
		return "", fmt.Errorf("create conf.d failed: %w", err)
	}
	return confDDir, nil
}

// listConfigFiles 列出目录下所有受支持的配置文件，并按文件名字典序升序返回
func listConfigFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read config dir failed: %w", err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !isSupportedConfigExt(entry.Name()) {
			continue
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}

	sort.Slice(files, func(i, j int) bool {
		return filepath.Base(files[i]) < filepath.Base(files[j])
	})

	return files, nil
}

// isConfigFilePath 判断给定路径是否指向受支持的配置文件
func isConfigFilePath(path string) bool {
	return isSupportedConfigExt(path)
}

// isSupportedConfigExt 判断路径扩展名是否为受支持的配置格式
func isSupportedConfigExt(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".toml" || ext == ".yaml" || ext == ".yml"
}
