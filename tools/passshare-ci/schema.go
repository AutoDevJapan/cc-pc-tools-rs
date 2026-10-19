package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
)

func compactSchema(value any) ([]byte, error) {
	var out bytes.Buffer
	e := json.NewEncoder(&out)
	e.SetEscapeHTML(false)
	if err := e.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

// Transfer only the four shared Tauri permission definitions observed in the
// real desktop generator diff. Reject unrelated changes and preserve grants.
func transferSharedDefinitions(oldDesktop, newDesktop, oldMobile []byte) ([]byte, error) {
	read := func(b []byte) (object, error) { var v object; err := json.Unmarshal(b, &v); return v, err }
	old, err := read(oldDesktop)
	if err != nil {
		return nil, err
	}
	new, err := read(newDesktop)
	if err != nil {
		return nil, err
	}
	mobile, err := read(oldMobile)
	if err != nil {
		return nil, err
	}
	var newRaw, mobileRaw map[string]json.RawMessage
	if err := json.Unmarshal(newDesktop, &newRaw); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(oldMobile, &mobileRaw); err != nil {
		return nil, err
	}
	result := append([]byte(nil), oldMobile...)
	for plugin, names := range map[string][]string{
		"core:app":    {"allow-exit", "deny-exit"},
		"core:window": {"allow-set-fullscreen-on-monitor", "deny-set-fullscreen-on-monitor"},
	} {
		oldPlugin, ok := old[plugin].(object)
		if !ok {
			return nil, fmt.Errorf("old shared plugin missing")
		}
		newPlugin, ok := new[plugin].(object)
		if !ok {
			return nil, fmt.Errorf("new shared plugin missing")
		}
		mobilePlugin, ok := mobile[plugin].(object)
		if !ok {
			return nil, fmt.Errorf("mobile shared plugin missing")
		}
		if !reflect.DeepEqual(oldPlugin, mobilePlugin) {
			return nil, fmt.Errorf("mobile shared definitions differ from the reviewed desktop baseline")
		}
		op := oldPlugin["permissions"].(object)
		np := newPlugin["permissions"].(object)
		mp := mobilePlugin["permissions"].(object)
		var newFields, mobileFields map[string]json.RawMessage
		if err := json.Unmarshal(newRaw[plugin], &newFields); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(mobileRaw[plugin], &mobileFields); err != nil {
			return nil, err
		}
		var newPermissions, mobilePermissions map[string]json.RawMessage
		if err := json.Unmarshal(newFields["permissions"], &newPermissions); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(mobileFields["permissions"], &mobilePermissions); err != nil {
			return nil, err
		}
		for _, name := range names {
			if _, exists := op[name]; exists {
				return nil, fmt.Errorf("permission already present")
			}
			v, exists := np[name]
			if !exists {
				return nil, fmt.Errorf("expected generated permission missing")
			}
			op[name] = v
			mp[name] = v
			mobilePermissions[name] = newPermissions[name]
		}
		// Permission names are a sorted map, but each definition is a Rust
		// struct whose original field order must remain byte-for-byte intact.
		permissions, err := compactSchema(mobilePermissions)
		if err != nil {
			return nil, err
		}
		oldPart := append([]byte(`"permissions":`), mobileFields["permissions"]...)
		newPart := append([]byte(`"permissions":`), permissions...)
		if bytes.Count(mobileRaw[plugin], oldPart) != 1 {
			return nil, fmt.Errorf("ambiguous permission object")
		}
		updatedPlugin := bytes.Replace(mobileRaw[plugin], oldPart, newPart, 1)
		key, _ := json.Marshal(plugin)
		prefix := append(key, ':')
		oldPart = append(append([]byte(nil), prefix...), mobileRaw[plugin]...)
		newPart = append(append([]byte(nil), prefix...), updatedPlugin...)
		if bytes.Count(result, oldPart) != 1 {
			return nil, fmt.Errorf("ambiguous plugin object")
		}
		result = bytes.Replace(result, oldPart, newPart, 1)
	}
	if !reflect.DeepEqual(old, new) {
		return nil, fmt.Errorf("real desktop generator diff exceeds the four expected definitions")
	}
	actual, err := read(result)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(actual, mobile) {
		return nil, fmt.Errorf("raw JSON update altered unrelated values")
	}
	return result, nil
}
