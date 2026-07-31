package compute

import (
	"encoding/json"
	"fmt"
	"hash/fnv"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// applyInstanceDiskFields sets canonical disks[] and echoes bootDisk / initializeParams on GET.
func applyInstanceDiskFields(project, zone, instanceID string, out map[string]any) {
	storedBoot, hasBoot := out["bootDisk"].(map[string]any)
	storedDisks, hasDisks := out["disks"].([]any)

	var disks []any
	switch {
	case hasDisks && len(storedDisks) > 0:
		disks = normalizeAttachedDisks(project, zone, instanceID, storedDisks)
	case hasBoot:
		disks = []any{attachedDiskFromBootDisk(project, zone, instanceID, storedBoot)}
	default:
		return
	}
	out["disks"] = disks

	if hasBoot {
		out["bootDisk"] = echoBootDisk(storedBoot, disks)
		return
	}
	if boot := firstBootDisk(disks); boot != nil {
		out["bootDisk"] = bootDiskFromAttached(boot)
	}
}

func normalizeAttachedDisks(project, zone, instanceID string, raw []any) []any {
	out := make([]any, 0, len(raw))
	for i, item := range raw {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		out = append(out, normalizeAttachedDisk(project, zone, instanceID, m, i))
	}
	return out
}

func normalizeAttachedDisk(project, zone, instanceID string, d map[string]any, index int) map[string]any {
	disk := cloneMap(d)
	if _, ok := disk["kind"]; !ok {
		disk["kind"] = "compute#attachedDisk"
	}
	if _, ok := disk["type"]; !ok {
		disk["type"] = "PERSISTENT"
	}
	if _, ok := disk["mode"]; !ok {
		disk["mode"] = "READ_WRITE"
	}
	if index == 0 {
		if _, ok := disk["boot"]; !ok {
			disk["boot"] = true
		}
	}
	if _, ok := disk["deviceName"]; !ok {
		name := instanceID
		if index > 0 {
			name = fmt.Sprintf("%s-%d", instanceID, index)
		}
		disk["deviceName"] = name
	}
	if ip, ok := disk["initializeParams"].(map[string]any); ok {
		disk["initializeParams"] = echoInitializeParams(project, zone, ip)
	}
	return disk
}

func attachedDiskFromBootDisk(project, zone, instanceID string, boot map[string]any) map[string]any {
	disk := map[string]any{
		"kind":       "compute#attachedDisk",
		"type":       "PERSISTENT",
		"mode":       "READ_WRITE",
		"boot":       true,
		"autoDelete": true,
		"deviceName": instanceID,
	}
	if v, ok := boot["autoDelete"]; ok {
		disk["autoDelete"] = v
	}
	if v, ok := boot["deviceName"]; ok {
		disk["deviceName"] = v
	}
	if ip, ok := boot["initializeParams"].(map[string]any); ok {
		disk["initializeParams"] = echoInitializeParams(project, zone, ip)
	} else if ip, ok := boot["initialize_params"].(map[string]any); ok {
		disk["initializeParams"] = echoInitializeParams(project, zone, ip)
	}
	return disk
}

func echoBootDisk(stored map[string]any, disks []any) map[string]any {
	out := cloneMap(stored)
	if boot := firstBootDisk(disks); boot != nil {
		if ip, ok := boot["initializeParams"].(map[string]any); ok {
			out["initializeParams"] = cloneMap(ip)
		}
	}
	return out
}

func bootDiskFromAttached(disk map[string]any) map[string]any {
	out := map[string]any{
		"autoDelete": disk["autoDelete"],
		"deviceName": disk["deviceName"],
	}
	if ip, ok := disk["initializeParams"].(map[string]any); ok {
		out["initializeParams"] = cloneMap(ip)
	}
	return out
}

func firstBootDisk(disks []any) map[string]any {
	for _, item := range disks {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		if boot, ok := m["boot"].(bool); ok && boot {
			return m
		}
	}
	if len(disks) > 0 {
		m, _ := disks[0].(map[string]any)
		return m
	}
	return nil
}

func echoInitializeParams(project, zone string, ip map[string]any) map[string]any {
	out := cloneMap(ip)
	image, hasImage := out["image"].(string)
	sourceImage, hasSource := out["sourceImage"].(string)
	switch {
	case hasImage && !hasSource:
		out["sourceImage"] = image
	case hasSource && !hasImage:
		out["image"] = sourceImage
	}
	if _, ok := out["diskType"]; !ok {
		out["diskType"] = selfLink("projects", project, "zones", zone, "diskTypes", "pd-balanced")
	}
	if _, ok := out["diskSizeGb"]; !ok {
		out["diskSizeGb"] = "10"
	}
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	raw, err := json.Marshal(in)
	if err != nil {
		out := make(map[string]any, len(in))
		for k, v := range in {
			out[k] = v
		}
		return out
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[string]any{}
	}
	return out
}

func instanceNumericID(inst store.GCEInstance) string {
	if inst.NumericID != "" {
		return inst.NumericID
	}
	return stableGCEInstanceNumericID(inst.Name)
}

func stableGCEInstanceNumericID(resourceName string) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(resourceName))
	return fmt.Sprintf("%d", h.Sum64()&0x7fffffffffffffff)
}
