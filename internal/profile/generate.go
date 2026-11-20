// Package profile: генераторы данных профиля (чистые функции) и настройка
// профиля аккаунта в Telegram. Все генераторы принимают *rand.Rand, поэтому
// с фиксированным seed результат воспроизводим и тестируется.
package profile

import (
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"strings"
	"time"
)

type Gender string

const (
	Male   Gender = "m"
	Female Gender = "f"
)

func ParseGender(s string) (Gender, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "m":
		return Male, nil
	case "f":
		return Female, nil
	}
	return "", fmt.Errorf("gender must be m or f, got %q", s)
}

// NewRand возвращает детерминированный генератор для данной строки
// (например, номера телефона): один и тот же аккаунт даёт одни и те же значения.
func NewRand(seed string) *rand.Rand {
	h1 := fnv.New64a()
	h1.Write([]byte(seed))
	h2 := fnv.New64a()
	h2.Write([]byte("tg-profile:" + seed))
	return rand.New(rand.NewPCG(h1.Sum64(), h2.Sum64()))
}

func FirstName(r *rand.Rand, g Gender) string {
	if g == Female {
		return femaleNames[r.IntN(len(femaleNames))]
	}
	return maleNames[r.IntN(len(maleNames))]
}

func LastName(r *rand.Rand) string { return lastNames[r.IntN(len(lastNames))] }

// Birthday возвращает дату, при которой на момент now возраст лежит в [minAge, maxAge].
func Birthday(r *rand.Rand, now time.Time, minAge, maxAge int) time.Time {
	now = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	latest := now.AddDate(-minAge, 0, 0)     // ровно minAge лет
	earliest := now.AddDate(-maxAge-1, 0, 1) // на день позже, чем maxAge+1 лет
	days := int(latest.Sub(earliest).Hours() / 24)
	return earliest.AddDate(0, 0, r.IntN(days+1))
}

func Bio(r *rand.Rand) string { return bios[r.IntN(len(bios))] }

// Username генерирует вариант username (5–32 символа, начинается с буквы).
// Пока не подключён к входу: username задаётся вручную в таблице.
func Username(r *rand.Rand, first, last string) string {
	f, l := sanitize(first), sanitize(last)
	if f == "" {
		f = "user"
	}
	if l == "" {
		l = "tg"
	}
	var u string
	switch r.IntN(3) {
	case 0:
		u = fmt.Sprintf("%s%s%d", f, l, r.IntN(90)+10)
	case 1:
		u = fmt.Sprintf("%s_%s", f, l)
	default:
		u = fmt.Sprintf("%s.%d", f[:1]+l, r.IntN(900)+100)
		u = strings.ReplaceAll(u, ".", "_")
	}
	if len(u) < 5 {
		u += fmt.Sprintf("%d", r.IntN(9000)+1000)
	}
	if len(u) > 32 {
		u = u[:32]
	}
	return strings.TrimRight(u, "_")
}

func sanitize(s string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(s) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	return b.String()
}
