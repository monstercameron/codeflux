//go:build js && wasm

package preferences

import "github.com/monstercameron/GoWebComponents/v5/interop"

func openBrowserBackend() (Backend, error) {
	storage, err := interop.GetLocalStorage()
	if err != nil {
		return nil, ErrStorageUnavailable
	}
	return storage, nil
}
