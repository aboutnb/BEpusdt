package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type MerchantNonce struct {
	ID        int64     `gorm:"column:id;primaryKey;autoIncrement"`
	KeyID     string    `gorm:"column:key_id;type:varchar(64);not null;uniqueIndex:idx_merchant_nonce"`
	Nonce     string    `gorm:"column:nonce;type:varchar(128);not null;uniqueIndex:idx_merchant_nonce"`
	ExpiresAt time.Time `gorm:"column:expires_at;not null;index"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
}

func (MerchantNonce) TableName() string { return "bep_merchant_nonce" }

func RegisterMerchantNonce(keyID, nonce string, expiresAt time.Time) error {
	now := time.Now()
	return Db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("expires_at < ?", now).Delete(&MerchantNonce{}).Error; err != nil {
			return err
		}
		if err := tx.Create(&MerchantNonce{KeyID: keyID, Nonce: nonce, ExpiresAt: expiresAt}).Error; err != nil {
			return errors.New("request nonce already used")
		}
		return nil
	})
}

func StartBuildMerchantOrder(p OrderParams) (Order, error) {
	var order Order
	buildMutex.Lock()
	defer buildMutex.Unlock()

	res := Db.Where("order_id = ?", p.OrderId).Order("id desc").Limit(1).Find(&order)
	if res.Error != nil {
		return order, res.Error
	}
	if res.RowsAffected > 0 {
		if err := validateMerchantOrderIdentity(order, p); err != nil {
			return order, err
		}
		return order, nil
	}

	trade, err := BuildTrade(p)
	if err != nil {
		return order, err
	}
	return BuildOrder(p, trade)
}

func validateMerchantOrderIdentity(order Order, p OrderParams) error {
	actualMoney, err := decimal.NewFromString(order.Money)
	if err != nil {
		return fmt.Errorf("stored order amount is invalid: %w", err)
	}
	rateIdentical := true
	if strings.TrimSpace(p.Rate) != "" {
		requestedRate, requestedErr := decimal.NewFromString(p.Rate)
		storedRate, storedErr := decimal.NewFromString(order.Rate)
		rateIdentical = requestedErr == nil && storedErr == nil && requestedRate.Equal(storedRate)
	}
	identical := order.ApiType == OrderApiTypeMerchant &&
		actualMoney.Equal(p.Money) &&
		order.Fiat == p.Fiat &&
		order.TradeType == p.TradeType &&
		order.NotifyUrl == p.NotifyUrl &&
		order.ReturnUrl == p.RedirectUrl &&
		rateIdentical
	if !identical {
		return fmt.Errorf("order_id %s already exists with different immutable parameters", p.OrderId)
	}
	return nil
}
