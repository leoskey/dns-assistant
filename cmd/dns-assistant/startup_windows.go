package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const startupRegistryPath = `Software\Microsoft\Windows\CurrentVersion\Run`
const startupValueName = "DNS Assistant"

type startupStore struct {
	keyPath string
	options desktopOptions
}

func (s startupStore) command() string { return `"` + s.options.Executable + `" autostart` }

func (s startupStore) value() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, s.keyPath, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer k.Close()
	value, _, err := k.GetStringValue(startupValueName)
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil
	}
	return value, err
}

func (s startupStore) enabled() (bool, error) {
	value, err := s.value()
	return strings.EqualFold(value, s.command()), err
}

func (s startupStore) set(enabled bool) error {
	value, err := s.value()
	if err != nil {
		return err
	}
	if value != "" && !strings.EqualFold(value, s.command()) {
		return fmt.Errorf("另一个安装目录已开启开机启动，请先在该程序的托盘中关闭")
	}
	if enabled {
		if len([]rune(s.command())) > 260 {
			return fmt.Errorf("程序路径太长，请移至较短的目录再开启开机启动")
		}
		content, err := json.Marshal(s.options)
		if err != nil {
			return err
		}
		path := filepath.Join(filepath.Dir(s.options.Executable), ".startup.json")
		file, err := os.CreateTemp(filepath.Dir(path), ".startup-*")
		if err != nil {
			return err
		}
		defer os.Remove(file.Name())
		_, writeErr := file.Write(content)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
		if err = os.Rename(file.Name(), path); err != nil {
			return err
		}
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, s.keyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if enabled {
		return k.SetStringValue(startupValueName, s.command())
	}
	if err = k.DeleteValue(startupValueName); errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}
