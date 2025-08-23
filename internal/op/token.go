package op

import (
	"github.com/OpenListTeam/OpenList/v4/internal/db"
	"github.com/OpenListTeam/OpenList/v4/internal/model"
	"github.com/pkg/errors"
)

func GetTokens() ([]model.Token, error) {
	items, err := db.GetTokens()
	if err != nil {
		return nil, err
	}
	return items, err
}

func GetTokenByKey(key string) (*model.Token, error) {
	item, err := db.GetTokenByKey(key)
	if err != nil {
		return nil, err
	}
	return item, nil
}

func SaveToken(item *model.Token) (err error) {
	// update
	if err = db.SaveToken(item); err != nil {
		return err
	}
	return nil
}

func DeleteTokenByKey(key string) error {
	_, err := GetTokenByKey(key)
	if err != nil {
		return errors.WithMessage(err, "failed to get token")
	}
	return db.DeleteTokenByKey(key)
}
