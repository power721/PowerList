package op

import (
	log "github.com/sirupsen/logrus"
	"sync"
)

type ValidateFunc func() error

var (
	validateFuncs []ValidateFunc
	validateMutex sync.Mutex
)

// RegisterValidateFunc registers a validation function for a storage type
// Called by driver packages in their init() functions
func RegisterValidateFunc(fn ValidateFunc) {
	validateMutex.Lock()
	defer validateMutex.Unlock()
	validateFuncs = append(validateFuncs, fn)
}

func ValidateStorages() {
	validateMutex.Lock()
	fns := make([]ValidateFunc, len(validateFuncs))
	copy(fns, validateFuncs)
	validateMutex.Unlock()

	var wg sync.WaitGroup
	for _, fn := range fns {
		wg.Add(1)
		go func(f ValidateFunc) {
			defer wg.Done()
			if err := f(); err != nil {
				log.Warnf("storage validation error: %v", err)
			}
		}(fn)
	}
	wg.Wait()
	log.Infof("=== validate storages completed ===")
}
