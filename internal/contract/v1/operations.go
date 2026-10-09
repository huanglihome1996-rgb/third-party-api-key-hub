package v1

// RecoveryOpenInput contains acknowledgements and version references, never secrets.
type RecoveryOpenInput struct {
	ExpectedRevision  int64                `json:"expected_revision"`
	AccountsVerified  bool                 `json:"accounts_verified"`
	OwnershipVerified bool                 `json:"ownership_verified"`
	ValuesVerified    bool                 `json:"values_verified"`
	Credentials       []CredentialRefInput `json:"credentials"`
}
type RecoveryStatusResult struct {
	Revision       int64  `json:"revision"`
	RecoveryClosed bool   `json:"recovery_closed"`
	AuditGap       bool   `json:"audit_gap"`
	Mode           string `json:"mode"`
}
type BackupResult struct {
	BackupID  string `json:"backup_id"`
	Revision  int64  `json:"revision"`
	AuditGap  bool   `json:"audit_gap"`
	CreatedAt string `json:"created_at"`
	Mode      string `json:"mode"`
}
type RetentionResult struct {
	Removed int64  `json:"removed"`
	Cutoff  string `json:"cutoff"`
	Mode    string `json:"mode"`
}

func init() {
	for _, op := range []string{"operations.backup", "operations.retention", "recovery.status", "recovery.open"} {
		knownOperations[op] = true
	}
}
