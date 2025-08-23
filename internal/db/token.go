package db

import (
	"fmt"

	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/pkg/errors"
)

func GetTokens() ([]model.Token, error) {
	var tokens []model.Token
	if err := db.Find(&tokens).Error; err != nil {
		return nil, errors.WithStack(err)
	}
	return tokens, nil
}

func GetTokenByKey(key string) (*model.Token, error) {
	var token model.Token
	if err := db.Where(fmt.Sprintf("%s = ?", columnName("key")), key).First(&token).Error; err != nil {
		return nil, errors.WithStack(err)
	}
	return &token, nil
}

func SaveTokens(items []model.Token) (err error) {
	return errors.WithStack(db.Save(items).Error)
}

func SaveToken(item *model.Token) error {
	return errors.WithStack(db.Save(item).Error)
}

func DeleteTokenByKey(key string) error {
	return errors.WithStack(db.Delete(&model.Token{Key: key}).Error)
}
