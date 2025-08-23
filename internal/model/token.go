package model

import "time"

type Token struct {
	Key       string    `json:"key" gorm:"primaryKey" binding:"required"` // unique key
	Value     string    `json:"value"`                                    // value
	AccountId int       `json:"accountId"`
	Modified  time.Time `json:"modified"`
}
