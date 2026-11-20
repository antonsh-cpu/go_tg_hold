package sheet

import (
	"context"
	"fmt"
	"strings"

	"google.golang.org/api/option"
	"google.golang.org/api/sheets/v4"
)

// Названия колонок (регистр в заголовке не важен). Недостающие колонки
// добавляются в конец строки заголовков автоматически.
const (
	ColNumber        = "number"
	ColPassword      = "password"
	ColUsername      = "username"
	ColAPIID         = "api_id"
	ColAPIHash       = "api_hash"
	ColProxyIP       = "proxy_ip"
	ColProxyPort     = "proxy_port"
	ColProxyLogin    = "proxy_login"
	ColProxyPass     = "proxy_pass"
	ColProxyExpires  = "proxy_expires"
	ColOrderID       = "order_id"
	ColDeviceModel   = "device_model"
	ColSystemVersion = "system_version"
	ColAppVersion    = "app_version"
	ColLangCode      = "lang_code"
	ColStatus        = "status"
	ColLastError     = "last_error"
	ColLastActive    = "last_active"
	ColResumeAfter   = "resume_after"
)

// Колонки профиля.
const (
	ColGender       = "gender" // m или f
	ColFirstName    = "first_name"
	ColLastName     = "last_name"
	ColBirthday     = "birthday" // 2006-01-02
	ColBio          = "bio"
	ColAvatarFile   = "avatar_file"
	ColProfileSteps = "profile_steps" // выполненные шаги через запятую
)

// Статусы аккаунта.
const (
	StatusProxyReady  = "proxy_ready"
	StatusProxyFailed = "proxy_failed"
	StatusActive      = "active"
	StatusFloodWait   = "flood_wait"
	StatusBanned      = "banned"
	StatusAuthLost    = "auth_lost"
	StatusLoginFailed = "login_failed"
)

var requiredColumns = []string{
	ColNumber, ColPassword, ColUsername, ColAPIID, ColAPIHash,
	ColProxyIP, ColProxyPort, ColProxyLogin, ColProxyPass, ColProxyExpires, ColOrderID,
	ColDeviceModel, ColSystemVersion, ColAppVersion, ColLangCode,
	ColStatus, ColLastError, ColLastActive, ColResumeAfter,
	ColGender, ColFirstName, ColLastName, ColBirthday, ColBio, ColAvatarFile, ColProfileSteps,
}

type Account struct {
	Row int // номер строки в листе, с 1

	Number, Password, Username string
	APIID, APIHash             string

	ProxyIP, ProxyPort, ProxyLogin, ProxyPass string
	ProxyExpires, OrderID                     string

	DeviceModel, SystemVersion, AppVersion, LangCode string

	Status string

	Gender, FirstName, LastName, Birthday, Bio, AvatarFile, ProfileSteps string
}

func (a Account) HasProxy() bool {
	return a.ProxyIP != "" && a.ProxyPort != "" && a.ProxyLogin != "" && a.ProxyPass != ""
}

// Digits — номер только из цифр (для имён файлов).
func (a Account) Digits() string {
	var b strings.Builder
	for _, r := range a.Number {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Phone — номер в формате +1555...
func (a Account) Phone() string { return "+" + a.Digits() }

type Store struct {
	svc  *sheets.Service
	id   string
	name string
	cols map[string]int // имя колонки -> индекс с 0
}

func New(ctx context.Context, credsFile, spreadsheetID, sheetName string) (*Store, error) {
	svc, err := sheets.NewService(ctx,
		option.WithCredentialsFile(credsFile),
		option.WithScopes(sheets.SpreadsheetsScope),
	)
	if err != nil {
		return nil, fmt.Errorf("sheet: create service: %w", err)
	}
	s := &Store{svc: svc, id: spreadsheetID, name: sheetName}
	if err := s.ensureHeader(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) ensureHeader(ctx context.Context) error {
	resp, err := s.svc.Spreadsheets.Values.Get(s.id, s.a1("1:1")).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("sheet: read header: %w", err)
	}
	var header []string
	if len(resp.Values) > 0 {
		for _, v := range resp.Values[0] {
			header = append(header, strings.TrimSpace(fmt.Sprint(v)))
		}
	}
	s.cols = make(map[string]int, len(requiredColumns))
	for i, h := range header {
		if h != "" {
			s.cols[strings.ToLower(h)] = i
		}
	}
	for _, must := range []string{ColNumber, ColPassword} {
		if _, ok := s.cols[must]; !ok {
			return fmt.Errorf("sheet: column %q not found in row 1 of %q", must, s.name)
		}
	}

	added := false
	for _, c := range requiredColumns {
		if _, ok := s.cols[c]; !ok {
			s.cols[c] = len(header)
			header = append(header, c)
			added = true
		}
	}
	if !added {
		return nil
	}
	row := make([]interface{}, len(header))
	for i, h := range header {
		row[i] = h
	}
	_, err = s.svc.Spreadsheets.Values.Update(s.id, s.a1("A1"),
		&sheets.ValueRange{Values: [][]interface{}{row}}).
		ValueInputOption("RAW").Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("sheet: write header: %w", err)
	}
	return nil
}

// Load читает все строки с непустым номером.
func (s *Store) Load(ctx context.Context) ([]Account, error) {
	resp, err := s.svc.Spreadsheets.Values.Get(s.id, s.a1("A2:ZZ")).Context(ctx).Do()
	if err != nil {
		return nil, fmt.Errorf("sheet: read accounts: %w", err)
	}
	var out []Account
	for i, r := range resp.Values {
		get := func(col string) string {
			idx := s.cols[col]
			if idx >= len(r) {
				return ""
			}
			return strings.TrimSpace(fmt.Sprint(r[idx]))
		}
		a := Account{
			Row:           i + 2,
			Number:        get(ColNumber),
			Password:      get(ColPassword),
			Username:      get(ColUsername),
			APIID:         get(ColAPIID),
			APIHash:       get(ColAPIHash),
			ProxyIP:       get(ColProxyIP),
			ProxyPort:     get(ColProxyPort),
			ProxyLogin:    get(ColProxyLogin),
			ProxyPass:     get(ColProxyPass),
			ProxyExpires:  get(ColProxyExpires),
			OrderID:       get(ColOrderID),
			DeviceModel:   get(ColDeviceModel),
			SystemVersion: get(ColSystemVersion),
			AppVersion:    get(ColAppVersion),
			LangCode:      get(ColLangCode),
			Status:        get(ColStatus),
			Gender:        strings.ToLower(get(ColGender)),
			FirstName:     get(ColFirstName),
			LastName:      get(ColLastName),
			Birthday:      get(ColBirthday),
			Bio:           get(ColBio),
			AvatarFile:    get(ColAvatarFile),
			ProfileSteps:  get(ColProfileSteps),
		}
		if a.Digits() == "" {
			continue
		}
		out = append(out, a)
	}
	return out, nil
}

// Update записывает значения в указанные колонки одной строки.
func (s *Store) Update(ctx context.Context, row int, fields map[string]string) error {
	data := make([]*sheets.ValueRange, 0, len(fields))
	for col, val := range fields {
		idx, ok := s.cols[col]
		if !ok {
			return fmt.Errorf("sheet: unknown column %q", col)
		}
		data = append(data, &sheets.ValueRange{
			Range:  s.a1(fmt.Sprintf("%s%d", colLetter(idx), row)),
			Values: [][]interface{}{{val}},
		})
	}
	_, err := s.svc.Spreadsheets.Values.BatchUpdate(s.id, &sheets.BatchUpdateValuesRequest{
		ValueInputOption: "RAW",
		Data:             data,
	}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("sheet: update row %d: %w", row, err)
	}
	return nil
}

func (s *Store) a1(rng string) string {
	return "'" + strings.ReplaceAll(s.name, "'", "''") + "'!" + rng
}

func colLetter(idx int) string {
	var out []byte
	for n := idx + 1; n > 0; n = (n - 1) / 26 {
		out = append([]byte{byte('A' + (n-1)%26)}, out...)
	}
	return string(out)
}
