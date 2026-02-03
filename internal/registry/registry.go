package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// PrinterType represents the type of printer.
type PrinterType string

const (
	PrinterTypeReceipt PrinterType = "receipt"
	PrinterTypeLabel   PrinterType = "label"
	PrinterTypeA4      PrinterType = "a4"
)

// PrinterInfo holds information about a registered printer.
type PrinterInfo struct {
	ID         string      `json:"id"`
	Type       PrinterType `json:"type"`
	DevicePath string      `json:"device_path"`
	Available  bool        `json:"available"`
}

// PrinterChanges represents changes in printer availability.
type PrinterChanges struct {
	Added   []*PrinterInfo
	Removed []*PrinterInfo
	Changed bool
}

// Registry manages available printers.
type Registry struct {
	mu             sync.RWMutex
	printers       map[string]*PrinterInfo
	previousState  map[string]bool // tracks previous availability state
}

// NewRegistry creates a new printer registry.
func NewRegistry() *Registry {
	return &Registry{
		printers:      make(map[string]*PrinterInfo),
		previousState: make(map[string]bool),
	}
}

// Register adds a printer to the registry.
func (r *Registry) Register(info PrinterInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.printers[info.ID] = &info
}

// Get retrieves a printer by ID.
func (r *Registry) Get(id string) (*PrinterInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	printer, ok := r.printers[id]
	if !ok {
		return nil, fmt.Errorf("printer not found: %s", id)
	}
	return printer, nil
}

// GetByType retrieves the first available printer of a given type.
func (r *Registry) GetByType(t PrinterType) (*PrinterInfo, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, p := range r.printers {
		if p.Type == t && p.Available {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no available printer of type: %s", t)
}

// List returns all registered printers.
func (r *Registry) List() []*PrinterInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*PrinterInfo, 0, len(r.printers))
	for _, p := range r.printers {
		result = append(result, p)
	}
	return result
}

// RefreshAvailability updates the availability status of all printers.
func (r *Registry) RefreshAvailability() {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, p := range r.printers {
		p.Available = isDeviceWritable(p.DevicePath)
	}
}

// Detect scans for known printer devices and registers them.
func (r *Registry) Detect() {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Known receipt printers
	receiptCandidates := []struct {
		id   string
		path string
	}{
		{"epson-receipt", "/dev/usb/epson_tmt20iii"},
	}

	// Known label printers
	labelCandidates := []struct {
		id   string
		path string
	}{
		{"brother-label", "/dev/usb/brother_ql800"},
	}

	// Collect known device paths to avoid registering duplicates
	knownPaths := make(map[string]bool)
	for _, c := range receiptCandidates {
		knownPaths[c.path] = true
	}
	for _, c := range labelCandidates {
		knownPaths[c.path] = true
	}

	// Also resolve symlinks for known paths to catch aliases
	for path := range knownPaths {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			knownPaths[resolved] = true
		}
	}

	// Add dynamic devices, skipping any that resolve to a known device
	for i, path := range globDevices("/dev/usb/lp*") {
		resolved, _ := filepath.EvalSymlinks(path)
		if knownPaths[path] || knownPaths[resolved] {
			continue
		}
		receiptCandidates = append(receiptCandidates, struct {
			id   string
			path string
		}{fmt.Sprintf("usb-lp%d", i), path})
	}

	for i, path := range globDevices("/dev/lp*") {
		resolved, _ := filepath.EvalSymlinks(path)
		if knownPaths[path] || knownPaths[resolved] {
			continue
		}
		receiptCandidates = append(receiptCandidates, struct {
			id   string
			path string
		}{fmt.Sprintf("lp%d", i), path})
	}

	// Register receipt printers
	for _, c := range receiptCandidates {
		if _, exists := r.printers[c.id]; !exists {
			r.printers[c.id] = &PrinterInfo{
				ID:         c.id,
				Type:       PrinterTypeReceipt,
				DevicePath: c.path,
				Available:  isDeviceWritable(c.path),
			}
		}
	}

	// Register label printers
	for _, c := range labelCandidates {
		if _, exists := r.printers[c.id]; !exists {
			r.printers[c.id] = &PrinterInfo{
				ID:         c.id,
				Type:       PrinterTypeLabel,
				DevicePath: c.path,
				Available:  isDeviceWritable(c.path),
			}
		}
	}
}

// Clear removes all printers from the registry.
func (r *Registry) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.printers = make(map[string]*PrinterInfo)
	r.previousState = make(map[string]bool)
}

// DetectChanges scans for printers and returns any changes since last check.
func (r *Registry) DetectChanges() PrinterChanges {
	r.mu.Lock()
	defer r.mu.Unlock()

	changes := PrinterChanges{}

	// Build current state by scanning devices
	currentDevices := make(map[string]struct {
		path    string
		ptype   PrinterType
		existed bool
	})

	// Known receipt printers
	receiptCandidates := []struct {
		id   string
		path string
	}{
		{"epson-receipt", "/dev/usb/epson_tmt20iii"},
	}

	// Known label printers
	labelCandidates := []struct {
		id   string
		path string
	}{
		{"brother-label", "/dev/usb/brother_ql800"},
	}

	// Collect known device paths to avoid registering duplicates
	knownPaths := make(map[string]bool)
	for _, c := range receiptCandidates {
		knownPaths[c.path] = true
	}
	for _, c := range labelCandidates {
		knownPaths[c.path] = true
	}

	// Also resolve symlinks for known paths to catch aliases
	for path := range knownPaths {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			knownPaths[resolved] = true
		}
	}

	// Add dynamic devices, skipping any that resolve to a known device
	for i, path := range globDevices("/dev/usb/lp*") {
		resolved, _ := filepath.EvalSymlinks(path)
		if knownPaths[path] || knownPaths[resolved] {
			continue
		}
		receiptCandidates = append(receiptCandidates, struct {
			id   string
			path string
		}{fmt.Sprintf("usb-lp%d", i), path})
	}

	for i, path := range globDevices("/dev/lp*") {
		resolved, _ := filepath.EvalSymlinks(path)
		if knownPaths[path] || knownPaths[resolved] {
			continue
		}
		receiptCandidates = append(receiptCandidates, struct {
			id   string
			path string
		}{fmt.Sprintf("lp%d", i), path})
	}

	// Check receipt printers
	for _, c := range receiptCandidates {
		available := isDeviceWritable(c.path)
		currentDevices[c.id] = struct {
			path    string
			ptype   PrinterType
			existed bool
		}{c.path, PrinterTypeReceipt, available}
	}

	// Check label printers
	for _, c := range labelCandidates {
		available := isDeviceWritable(c.path)
		currentDevices[c.id] = struct {
			path    string
			ptype   PrinterType
			existed bool
		}{c.path, PrinterTypeLabel, available}
	}

	// Find added printers (now available, wasn't before)
	for id, dev := range currentDevices {
		wasAvailable := r.previousState[id]
		if dev.existed && !wasAvailable {
			printer := &PrinterInfo{
				ID:         id,
				Type:       dev.ptype,
				DevicePath: dev.path,
				Available:  true,
			}
			r.printers[id] = printer
			changes.Added = append(changes.Added, printer)
			changes.Changed = true
		} else if dev.existed {
			// Update existing printer
			if p, ok := r.printers[id]; ok {
				p.Available = true
			} else {
				r.printers[id] = &PrinterInfo{
					ID:         id,
					Type:       dev.ptype,
					DevicePath: dev.path,
					Available:  true,
				}
			}
		}
	}

	// Find removed printers (was available, not anymore)
	for id, wasAvailable := range r.previousState {
		if !wasAvailable {
			continue
		}
		dev, exists := currentDevices[id]
		if !exists || !dev.existed {
			if p, ok := r.printers[id]; ok {
				p.Available = false
				changes.Removed = append(changes.Removed, p)
				changes.Changed = true
			}
		}
	}

	// Update previous state
	r.previousState = make(map[string]bool)
	for id, dev := range currentDevices {
		r.previousState[id] = dev.existed
	}

	return changes
}

// GetAvailablePrinters returns all currently available printers.
func (r *Registry) GetAvailablePrinters() []*PrinterInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*PrinterInfo, 0)
	for _, p := range r.printers {
		if p.Available {
			result = append(result, p)
		}
	}
	return result
}

func globDevices(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return matches
}

func isDeviceWritable(path string) bool {
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
