//go:build windows

package storage

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// restrictToCurrentUser gives the path a protected DACL naming only the
// process's own user, and removes every inherited entry.
//
// Go's os.Chmod on Windows sets only the read-only attribute, so the POSIX
// mode arguments elsewhere in this package are inert here. Without this, the
// database, its write-ahead log, its shared-memory file, its backups, and the
// migration lock inherit whatever the parent directory allowed — typically
// Users on a default profile. That is a real exposure and not a portability
// footnote: the plan makes SQLite the sole authoritative store for threads,
// prompts, evidence, and budgets, and the primary supported platform is
// Windows.
//
// Administrators and SYSTEM are deliberately absent. They can take ownership
// regardless, so naming them would widen the grant without adding a capability
// anyone lacked, and it would make the assertion "only this user is named"
// untestable.
func restrictToCurrentUser(path string) error {
	// A path that does not exist yet has nothing to protect. Callers apply
	// this after creation, and SQLite's sidecar files appear only once the
	// database is opened, so absence is expected rather than exceptional.
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return classify("inspect path before restricting it", err)
	}

	user, err := currentUserSID()
	if err != nil {
		return err
	}

	access := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		// Inheritance matters for the application-data directory: without it,
		// files SQLite creates later come back with the default DACL.
		Inheritance: windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(user),
		},
	}}

	acl, err := windows.ACLFromEntries(access, nil)
	if err != nil {
		return classify("build access control list", err)
	}

	// PROTECTED_DACL_SECURITY_INFORMATION is the load-bearing flag. Setting a
	// DACL without it leaves inherited entries in place, so the path would
	// still be reachable by whoever the parent granted.
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	); err != nil {
		return classify("restrict "+path+" to the current user", err)
	}
	return nil
}

// currentUserSID returns the SID of the account this process runs as.
func currentUserSID() (*windows.SID, error) {
	token := windows.GetCurrentProcessToken()
	user, err := token.GetTokenUser()
	if err != nil {
		return nil, classify("resolve process user", err)
	}
	if user.User.Sid == nil {
		return nil, classify(
			"resolve process user",
			fmt.Errorf("process token carries no user SID"),
		)
	}
	return user.User.Sid, nil
}

// namedTrusteesFor reports every trustee named by the path's DACL, as SID
// strings. It exists so a test can assert the grant rather than assume it.
func namedTrusteesFor(path string) ([]string, error) {
	descriptor, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return nil, fmt.Errorf("read security information for %s: %w", path, err)
	}
	acl, _, err := descriptor.DACL()
	if err != nil {
		return nil, fmt.Errorf("read discretionary access control list: %w", err)
	}
	if acl == nil {
		// A nil DACL grants everyone full control. Reporting it as "no
		// trustees" would read as restrictive when it is the opposite.
		return nil, fmt.Errorf("%s has a null DACL, which grants everyone access", path)
	}

	var trustees []string
	for index := uint32(0); index < uint32(acl.AceCount); index++ {
		var entry *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, index, &entry); err != nil {
			return nil, fmt.Errorf("read access entry %d: %w", index, err)
		}
		sid := (*windows.SID)(unsafe.Pointer(&entry.SidStart))
		trustees = append(trustees, sid.String())
	}
	return trustees, nil
}
