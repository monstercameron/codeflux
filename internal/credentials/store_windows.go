//go:build windows

package credentials

import (
	"context"
	"errors"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	credentialTypeGeneric         = 1
	credentialPersistLocalMachine = 2
)

var (
	advapi32        = windows.NewLazySystemDLL("advapi32.dll")
	procCredWriteW  = advapi32.NewProc("CredWriteW")
	procCredReadW   = advapi32.NewProc("CredReadW")
	procCredDeleteW = advapi32.NewProc("CredDeleteW")
	procCredFree    = advapi32.NewProc("CredFree")
)

type nativeCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

type credentialManagerAPI interface {
	Write(target, account string, value []byte) error
	Read(target string) ([]byte, error)
	Delete(target string) error
}

// WindowsStore persists generic credentials in the current user's Windows
// Credential Manager vault.
type WindowsStore struct {
	api credentialManagerAPI
}

// NewPlatformStore returns the native Windows credential store.
func NewPlatformStore() Store {
	return &WindowsStore{api: nativeWindowsCredentialAPI{}}
}

// PlatformStatus reports the safe native backend identity.
func PlatformStatus() (bool, string) {
	return true, "Windows Credential Manager"
}

func (store *WindowsStore) Create(
	ctx context.Context,
	reference Reference,
	secret Secret,
) error {
	if err := validateOperation(ctx, reference); err != nil {
		return err
	}
	if _, err := store.api.Read(targetName(reference)); err == nil {
		return ErrAlreadyExists
	} else if !errors.Is(err, ErrNotFound) {
		return err
	}
	return secret.Use(func(value []byte) error {
		return store.api.Write(targetName(reference), reference.Account, value)
	})
}

func (store *WindowsStore) Update(
	ctx context.Context,
	reference Reference,
	secret Secret,
) error {
	if err := validateOperation(ctx, reference); err != nil {
		return err
	}
	existing, err := store.api.Read(targetName(reference))
	zero(existing)
	if err != nil {
		return err
	}
	return secret.Use(func(value []byte) error {
		return store.api.Write(targetName(reference), reference.Account, value)
	})
}

func (store *WindowsStore) Retrieve(
	ctx context.Context,
	reference Reference,
) (Secret, error) {
	if err := validateOperation(ctx, reference); err != nil {
		return Secret{}, err
	}
	value, err := store.api.Read(targetName(reference))
	if err != nil {
		return Secret{}, err
	}
	defer zero(value)
	return NewSecret(value)
}

func (store *WindowsStore) Test(
	ctx context.Context,
	reference Reference,
) error {
	secret, err := store.Retrieve(ctx, reference)
	if err != nil {
		return err
	}
	secret.Destroy()
	return nil
}

func (store *WindowsStore) Delete(
	ctx context.Context,
	reference Reference,
) error {
	if err := validateOperation(ctx, reference); err != nil {
		return err
	}
	return store.api.Delete(targetName(reference))
}

type nativeWindowsCredentialAPI struct{}

func (nativeWindowsCredentialAPI) Write(target, account string, value []byte) error {
	targetPointer, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return ErrUnavailable
	}
	accountPointer, err := windows.UTF16PtrFromString(account)
	if err != nil {
		return ErrUnavailable
	}
	credential := nativeCredential{
		Type:               credentialTypeGeneric,
		TargetName:         targetPointer,
		CredentialBlobSize: uint32(len(value)),
		CredentialBlob:     &value[0],
		Persist:            credentialPersistLocalMachine,
		UserName:           accountPointer,
	}
	result, _, callErr := procCredWriteW.Call(
		uintptr(unsafe.Pointer(&credential)),
		0,
	)
	runtime.KeepAlive(value)
	runtime.KeepAlive(targetPointer)
	runtime.KeepAlive(accountPointer)
	if result == 0 {
		return classifyWindowsCredentialError(callErr)
	}
	return nil
}

func (nativeWindowsCredentialAPI) Read(target string) ([]byte, error) {
	targetPointer, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return nil, ErrUnavailable
	}
	var credentialPointer *nativeCredential
	result, _, callErr := procCredReadW.Call(
		uintptr(unsafe.Pointer(targetPointer)),
		credentialTypeGeneric,
		0,
		uintptr(unsafe.Pointer(&credentialPointer)),
	)
	runtime.KeepAlive(targetPointer)
	if result == 0 {
		return nil, classifyWindowsCredentialError(callErr)
	}
	defer procCredFree.Call(uintptr(unsafe.Pointer(credentialPointer)))
	if credentialPointer == nil ||
		credentialPointer.CredentialBlob == nil ||
		credentialPointer.CredentialBlobSize == 0 {
		return nil, ErrNotFound
	}
	value := append(
		[]byte(nil),
		unsafe.Slice(
			credentialPointer.CredentialBlob,
			int(credentialPointer.CredentialBlobSize),
		)...,
	)
	return value, nil
}

func (nativeWindowsCredentialAPI) Delete(target string) error {
	targetPointer, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return ErrUnavailable
	}
	result, _, callErr := procCredDeleteW.Call(
		uintptr(unsafe.Pointer(targetPointer)),
		credentialTypeGeneric,
		0,
	)
	runtime.KeepAlive(targetPointer)
	if result == 0 {
		return classifyWindowsCredentialError(callErr)
	}
	return nil
}

func validateOperation(ctx context.Context, reference Reference) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err := NewReference(reference.Service, reference.Account)
	return err
}

func targetName(reference Reference) string {
	return "Codeflux/" + reference.Service + "/" + reference.Account
}

func classifyWindowsCredentialError(err error) error {
	if errors.Is(err, syscall.Errno(windows.ERROR_NOT_FOUND)) {
		return ErrNotFound
	}
	if err == nil || errors.Is(err, syscall.Errno(0)) {
		return ErrUnavailable
	}
	return errors.Join(ErrUnavailable, err)
}
