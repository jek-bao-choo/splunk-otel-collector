// Copyright Splunk, Inc.
// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package configprovider

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cast"
	"go.opentelemetry.io/collector/config"
	expcfg "go.opentelemetry.io/collector/config/experimental/config"
	"go.opentelemetry.io/collector/config/experimental/configsource"
)

const (
	configSourcesKey = "config_sources"
)

// Private error types to help with testability.
type (
	errInvalidTypeAndNameKey struct{ error }
	errUnknownType           struct{ error }
	errUnmarshalError        struct{ error }
	errDuplicateName         struct{ error }
)

// Load reads the configuration for ConfigSource objects from the given parser and returns a map
// from the full name of config sources to the respective ConfigSettings.
func Load(ctx context.Context, v *config.Map, factories Factories) (map[string]expcfg.Source, error) {
	processedParser, err := processParser(ctx, v)
	if err != nil {
		return nil, err
	}

	cfgSrcSettings, err := loadSettings(cast.ToStringMap(processedParser.Get(configSourcesKey)), factories)
	if err != nil {
		return nil, err
	}

	return cfgSrcSettings, nil
}

// processParser prepares a config.Map to be used to load config source settings.
func processParser(ctx context.Context, v *config.Map) (*config.Map, error) {
	// Use a manager to resolve environment variables with a syntax consistent with
	// the config source usage.
	manager := newManager(make(map[string]configsource.ConfigSource))
	defer func() {
		_ = manager.Close(ctx)
	}()

	processedParser := config.NewMap()
	for _, key := range v.AllKeys() {
		if !strings.HasPrefix(key, configSourcesKey) {
			// In Load we only care about config sources, ignore everything else.
			continue
		}

		value, err := manager.parseConfigValue(ctx, v.Get(key))
		if err != nil {
			return nil, err
		}
		processedParser.Set(key, value)
	}

	return processedParser, nil
}

func loadSettings(css map[string]interface{}, factories Factories) (map[string]expcfg.Source, error) {
	// Prepare resulting map.
	cfgSrcToSettings := make(map[string]expcfg.Source)

	// Iterate over extensions and create a config for each.
	for key, value := range css {
		settingsParser := config.NewMapFromStringMap(cast.ToStringMap(value))

		// Decode the key into type and fullName components.
		componentID, err := config.NewComponentIDFromString(key)
		if err != nil {
			return nil, &errInvalidTypeAndNameKey{fmt.Errorf("invalid %s type and name key %q: %w", configSourcesKey, key, err)}
		}

		// Find the factory based on "type" that we read from config source.
		factory := factories[componentID.Type()]
		if factory == nil {
			return nil, &errUnknownType{fmt.Errorf("unknown %s type %q for %q", configSourcesKey, componentID.Type(), componentID)}
		}

		// Create the default config.
		cfgSrcSettings := factory.CreateDefaultConfig()
		cfgSrcSettings.SetIDName(componentID.Name())

		// Now that the default settings struct is created we can Unmarshal into it
		// and it will apply user-defined config on top of the default.
		if err = settingsParser.UnmarshalExact(&cfgSrcSettings); err != nil {
			return nil, &errUnmarshalError{fmt.Errorf("error reading %s configuration for %q: %w", configSourcesKey, componentID, err)}
		}

		fullName := componentID.String()
		if cfgSrcToSettings[fullName] != nil {
			return nil, &errDuplicateName{fmt.Errorf("duplicate %s name %s", configSourcesKey, fullName)}
		}

		cfgSrcToSettings[fullName] = cfgSrcSettings
	}

	return cfgSrcToSettings, nil
}
