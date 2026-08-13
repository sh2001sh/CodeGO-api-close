package app

import (
	"errors"
	"time"

	luckysettings "github.com/sh2001sh/new-api/internal/commerce/luckysettings"
	commerceschema "github.com/sh2001sh/new-api/internal/commerce/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func issueBlindBoxDailyLuckyNumberTx(tx *gorm.DB, record *commerceschema.BlindBoxOpenRecord) error {
	if tx == nil || record == nil || record.Id <= 0 || record.UserId <= 0 {
		return errors.New("invalid blind box lucky number params")
	}
	setting := luckysettings.Get()
	location, err := setting.Location()
	if err != nil {
		return err
	}
	now := time.Now().In(location)
	drawDate, expiresAt := blindBoxLuckyDrawWindow(now, setting)

	number := commerceschema.BlindBoxDailyLuckyNumber{
		BlindBoxOpenRecordId: record.Id,
		UserId:               record.UserId,
		DrawDate:             drawDate,
		ExpiresAt:            expiresAt,
	}
	for attempt := 0; attempt < 8; attempt++ {
		number.LuckySuffix, err = generateLuckyNumber()
		if err != nil {
			return err
		}
		result := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&number)
		if result.Error != nil {
			return result.Error
		}
		if number.Id > 0 {
			decorateBlindBoxLuckyNumber(record, &number)
			return nil
		}
		if err := tx.Where("blind_box_open_record_id = ?", record.Id).First(&number).Error; err == nil {
			decorateBlindBoxLuckyNumber(record, &number)
			return nil
		}
	}
	return errors.New("could not issue blind box daily lucky number")
}

// blindBoxLuckyDrawWindow assigns an opening to the next scheduled draw.
// The draw instant itself starts the following draw's eligibility window.
func blindBoxLuckyDrawWindow(now time.Time, setting luckysettings.Setting) (string, int64) {
	location, err := setting.Location()
	if err == nil {
		now = now.In(location)
	}
	drawAt := time.Date(
		now.Year(), now.Month(), now.Day(),
		setting.DrawHour, setting.DrawMinute, 0, 0, now.Location(),
	)
	if !now.Before(drawAt) {
		drawAt = drawAt.AddDate(0, 0, 1)
	}
	return drawAt.Format(luckyDrawDateLayout), drawAt.Unix()
}

func createBlindBoxOpenRecordTx(tx *gorm.DB, record *commerceschema.BlindBoxOpenRecord) error {
	if err := tx.Create(record).Error; err != nil {
		return err
	}
	return issueBlindBoxDailyLuckyNumberTx(tx, record)
}

func decorateBlindBoxLuckyNumber(record *commerceschema.BlindBoxOpenRecord, number *commerceschema.BlindBoxDailyLuckyNumber) {
	if record == nil || number == nil {
		return
	}
	record.LuckyNumber = number.LuckySuffix
	record.LuckyDrawDate = number.DrawDate
	record.LuckyExpiresAt = number.ExpiresAt
}

func attachBlindBoxLuckyNumbersTx(tx *gorm.DB, records []commerceschema.BlindBoxOpenRecord) error {
	if len(records) == 0 || platformdb.DB == nil || !platformdb.DB.Migrator().HasTable(&commerceschema.BlindBoxDailyLuckyNumber{}) {
		return nil
	}
	ids := make([]int, 0, len(records))
	for _, record := range records {
		ids = append(ids, record.Id)
	}
	var numbers []commerceschema.BlindBoxDailyLuckyNumber
	if err := tx.Where("blind_box_open_record_id IN ?", ids).Find(&numbers).Error; err != nil {
		return err
	}
	byRecord := make(map[int]commerceschema.BlindBoxDailyLuckyNumber, len(numbers))
	for _, number := range numbers {
		byRecord[number.BlindBoxOpenRecordId] = number
	}
	for index := range records {
		if number, ok := byRecord[records[index].Id]; ok {
			decorateBlindBoxLuckyNumber(&records[index], &number)
		}
	}
	return nil
}
