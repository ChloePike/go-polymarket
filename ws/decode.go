// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 ChloePike

package ws

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// splitFrame reports the one or more top-level JSON objects a raw inbound
// text frame contains. The market channel's initial book-snapshot dump is
// the only case observed live where a frame is a top-level JSON array
// (one book object per subscribed asset); every other event type observed
// arrives as a single bare JSON object. A robust decoder accepts both
// shapes at the top level of any frame, so this helper normalizes them
// into a slice either way.
func splitFrame(frame []byte) ([]json.RawMessage, error) {
	trimmed := bytes.TrimSpace(frame)
	if len(trimmed) == 0 {
		return nil, nil
	}
	switch trimmed[0] {
	case '[':
		var arr []json.RawMessage
		if err := json.Unmarshal(trimmed, &arr); err != nil {
			return nil, fmt.Errorf("ws: decode frame array: %w", err)
		}
		return arr, nil
	case '{':
		return []json.RawMessage{json.RawMessage(trimmed)}, nil
	default:
		return nil, fmt.Errorf("ws: unrecognized frame (starts with %q)", trimmed[:1])
	}
}

// sortedKeys returns m's keys in ascending order, so a subscription's
// serialized frame is deterministic regardless of Go's randomized map
// iteration order.
func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
