package domain

import (
	"errors"
	"time"
)

var (
	ErrConflict          = errors.New("conflict")
	ErrNotFound          = errors.New("not found")
	ErrMappingIncomplete = errors.New("mapping incomplete")
	ErrMappingMismatch   = errors.New("mapping mismatch")
	ErrMissing           = errors.New("credential missing")
	ErrExpired           = errors.New("credential expired")
	ErrDisabled          = errors.New("credential disabled")
	ErrSelection         = errors.New("selection required")
	ErrGeneration        = errors.New("generation conflict")
	ErrRecovery          = errors.New("recovery not eligible")
	ErrInvalid           = errors.New("invalid input")
	ErrPermission        = errors.New("permission denied")
)

type App struct {
	ID                 string `json:"app_id"`
	Platform           string `json:"platform"`
	ProviderAppID      string `json:"provider_app_id"`
	Name               string `json:"name"`
	RowRevision        int64  `json:"row_revision"`
	AuthorizationEpoch int64  `json:"authorization_epoch"`
}
type ExternalShop struct {
	ID             string `json:"external_shop_id"`
	Platform       string `json:"platform"`
	ProviderShopID string `json:"provider_shop_id"`
	Name           string `json:"name"`
	BrandID        string `json:"brand_id"`
	RowRevision    int64  `json:"row_revision"`
}
type Brand struct {
	ID             string `json:"brand_id"`
	Name           string `json:"name"`
	InternalShopID string `json:"internal_shop_id"`
	RowRevision    int64  `json:"row_revision"`
}
type InternalShop struct {
	ID          string `json:"internal_shop_id"`
	Name        string `json:"name"`
	RowRevision int64  `json:"row_revision"`
}
type Installation struct {
	AppID          string `json:"app_id"`
	ExternalShopID string `json:"external_shop_id"`
	RowRevision    int64  `json:"row_revision"`
}
type Credential struct {
	Scope, AppID, ExternalShopID, Type, Ciphertext string
	Generation, RowRevision                        int64
	ExpiryKnown                                    bool
	ExpiresAt                                      *time.Time
	ConfirmedGeneration, ValidationGeneration      int64
	ValidationResult                               string
	Disabled                                       bool
	DisabledGeneration, RecoveryBasisGeneration    int64
	RecoveryBasisOperationID, ValueOrigin          string
}

func (c Credential) Available(now time.Time) error {
	if c.Disabled {
		return ErrDisabled
	}
	if c.ExpiryKnown && (c.ExpiresAt == nil || !c.ExpiresAt.After(now)) {
		return ErrExpired
	}
	if c.ValidationResult == "invalid" && c.ValidationGeneration == c.Generation {
		return ErrMissing
	}
	if !c.ExpiryKnown && c.ConfirmedGeneration != c.Generation {
		return ErrMissing
	}
	if c.ConfirmedGeneration != c.Generation && !(c.ValidationResult == "valid" && c.ValidationGeneration == c.Generation) {
		return ErrMissing
	}
	return nil
}
func (c Credential) CanRestore() bool {
	return c.Disabled && c.RecoveryBasisGeneration > c.DisabledGeneration && c.RecoveryBasisGeneration <= c.Generation && c.RecoveryBasisOperationID != ""
}
