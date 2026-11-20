package profile

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/gotd/td/telegram/uploader"
	"github.com/gotd/td/tg"

	"go_tg_hold/internal/config"
	"go_tg_hold/internal/sheet"
	"go_tg_hold/internal/tgclient"
)

// Шаги профиля. Каждый выполняется один раз и отмечается в колонке profile_steps.
const (
	stepProfile  = "profile"  // имя, фамилия, bio
	stepBirthday = "birthday" // дата рождения
	stepPhoto    = "photo"    // аватар из папки images
	stepPrivacy  = "privacy"  // приватность
)

var allSteps = []string{stepProfile, stepBirthday, stepPhoto, stepPrivacy}

type Setup struct {
	cfg   *config.Config
	store *sheet.Store
}

func New(cfg *config.Config, store *sheet.Store) *Setup {
	return &Setup{cfg: cfg, store: store}
}

// Run настраивает профиль у всех active-аккаунтов, где остались невыполненные шаги.
func (s *Setup) Run(ctx context.Context) error {
	accounts, err := s.store.Load(ctx)
	if err != nil {
		return err
	}
	used := map[string]bool{}
	for _, a := range accounts {
		if a.AvatarFile != "" {
			used[a.AvatarFile] = true
		}
	}

	var todo []sheet.Account
	var problems []string
	skipped := 0
	for _, a := range accounts {
		if len(parseSteps(a.ProfileSteps)) == len(allSteps) {
			continue
		}
		if a.Status != sheet.StatusActive {
			skipped++
			continue
		}
		if _, err := ParseGender(a.Gender); err != nil {
			problems = append(problems, a.Number+": "+err.Error())
			continue
		}
		todo = append(todo, a)
	}
	if len(problems) > 0 {
		return fmt.Errorf("profile: fix the sheet first:\n  %s", strings.Join(problems, "\n  "))
	}
	if skipped > 0 {
		fmt.Printf("Skipped %d account(s) that are not active yet (run login first).\n", skipped)
	}
	if len(todo) == 0 {
		fmt.Println("No accounts need profile setup.")
		return nil
	}

	for i, a := range todo {
		if i > 0 {
			if err := sleep(ctx, time.Duration(s.cfg.PauseBetweenProfiles)*time.Second); err != nil {
				return err
			}
		}
		fmt.Printf("\n=== %s (row %d) ===\n", a.Number, a.Row)
		if err := s.setupOne(ctx, a, used); err != nil {
			fmt.Printf("FAILED: %v\n", err)
			if ctx.Err() != nil {
				return ctx.Err()
			}
		}
	}
	return nil
}

func (s *Setup) setupOne(ctx context.Context, a sheet.Account, used map[string]bool) error {
	g, _ := ParseGender(a.Gender) // проверено в Run
	r := NewRand(a.Digits())

	// Данные профиля генерируются один раз и сначала записываются в таблицу.
	fields := map[string]string{}
	if a.FirstName == "" {
		a.FirstName = FirstName(r, g)
		fields[sheet.ColFirstName] = a.FirstName
	}
	if a.LastName == "" {
		a.LastName = LastName(r)
		fields[sheet.ColLastName] = a.LastName
	}
	if a.Birthday == "" {
		a.Birthday = Birthday(r, time.Now(), s.cfg.MinAge, s.cfg.MaxAge).Format("2006-01-02")
		fields[sheet.ColBirthday] = a.Birthday
	}
	if a.Bio == "" {
		a.Bio = Bio(r)
		fields[sheet.ColBio] = a.Bio
	}
	if a.AvatarFile == "" {
		p, err := pickAvatar(s.cfg.ImagesDir, g, used, r)
		if err != nil {
			return s.fail(ctx, a, err)
		}
		a.AvatarFile = p
		used[p] = true
		fields[sheet.ColAvatarFile] = p
	}
	if len(fields) > 0 {
		if err := s.store.Update(ctx, a.Row, fields); err != nil {
			return err
		}
	}

	client, err := tgclient.New(s.cfg, a)
	if err != nil {
		return s.fail(ctx, a, err)
	}
	runErr := client.Run(ctx, func(ctx context.Context) error {
		st, err := client.Auth().Status(ctx)
		if err != nil {
			return err
		}
		if !st.Authorized {
			return errors.New("session is not authorized, run login again")
		}
		return s.runSteps(ctx, a, client.API())
	})
	if runErr != nil {
		return s.fail(ctx, a, runErr)
	}
	return s.store.Update(ctx, a.Row, map[string]string{
		sheet.ColLastError:  "",
		sheet.ColLastActive: time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Setup) runSteps(ctx context.Context, a sheet.Account, api *tg.Client) error {
	done := parseSteps(a.ProfileSteps)
	steps := []struct {
		name string
		fn   func(context.Context) error
	}{
		{stepProfile, func(ctx context.Context) error { return updateProfile(ctx, api, a) }},
		{stepBirthday, func(ctx context.Context) error { return updateBirthday(ctx, api, a.Birthday) }},
		{stepPhoto, func(ctx context.Context) error { return uploadPhoto(ctx, api, a.AvatarFile) }},
		{stepPrivacy, func(ctx context.Context) error { return applyPrivacy(ctx, api) }},
	}
	for _, st := range steps {
		if done[st.name] {
			continue
		}
		fmt.Printf("  step %-8s ...", st.name)
		if err := st.fn(ctx); err != nil {
			fmt.Println(" failed")
			return fmt.Errorf("step %s: %w", st.name, err)
		}
		fmt.Println(" ok")
		done[st.name] = true
		if err := s.store.Update(ctx, a.Row, map[string]string{sheet.ColProfileSteps: joinSteps(done)}); err != nil {
			return err
		}
		// Небольшая человеческая пауза между шагами.
		if err := sleep(ctx, time.Duration(3+rand.IntN(6))*time.Second); err != nil {
			return err
		}
	}
	return nil
}

func updateProfile(ctx context.Context, api *tg.Client, a sheet.Account) error {
	req := &tg.AccountUpdateProfileRequest{}
	req.SetFirstName(a.FirstName)
	req.SetLastName(a.LastName)
	req.SetAbout(a.Bio)
	_, err := api.AccountUpdateProfile(ctx, req)
	return err
}

// Нужна версия gotd/td со слоем, где есть account.updateBirthday.
func updateBirthday(ctx context.Context, api *tg.Client, value string) error {
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return fmt.Errorf("bad birthday %q: %w", value, err)
	}
	b := tg.Birthday{Day: t.Day(), Month: int(t.Month())}
	b.SetYear(t.Year())
	req := &tg.AccountUpdateBirthdayRequest{}
	req.SetBirthday(b)
	_, err = api.AccountUpdateBirthday(ctx, req)
	return err
}

func uploadPhoto(ctx context.Context, api *tg.Client, path string) error {
	file, err := uploader.NewUploader(api).FromPath(ctx, path)
	if err != nil {
		return fmt.Errorf("upload %s: %w", path, err)
	}
	req := &tg.PhotosUploadProfilePhotoRequest{}
	req.SetFile(file)
	_, err = api.PhotosUploadProfilePhoto(ctx, req)
	return err
}

// applyPrivacy: номер скрыт, последний визит только контактам, фото и приглашения в группы всем.
func applyPrivacy(ctx context.Context, api *tg.Client) error {
	settings := []struct {
		key  tg.InputPrivacyKeyClass
		rule tg.InputPrivacyRuleClass
	}{
		{&tg.InputPrivacyKeyPhoneNumber{}, &tg.InputPrivacyValueDisallowAll{}},
		{&tg.InputPrivacyKeyAddedByPhone{}, &tg.InputPrivacyValueDisallowAll{}},
		{&tg.InputPrivacyKeyStatusTimestamp{}, &tg.InputPrivacyValueAllowContacts{}},
		{&tg.InputPrivacyKeyProfilePhoto{}, &tg.InputPrivacyValueAllowAll{}},
		{&tg.InputPrivacyKeyChatInvite{}, &tg.InputPrivacyValueAllowAll{}},
	}
	for _, p := range settings {
		_, err := api.AccountSetPrivacy(ctx, &tg.AccountSetPrivacyRequest{
			Key:   p.key,
			Rules: []tg.InputPrivacyRuleClass{p.rule},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// pickAvatar выбирает свободную картинку из images/<m|f>, а если такой папки нет, из images.
func pickAvatar(dir string, g Gender, used map[string]bool, r *rand.Rand) (string, error) {
	candidates := []string{filepath.Join(dir, string(g)), dir}
	for _, d := range candidates {
		entries, err := os.ReadDir(d)
		if err != nil {
			continue
		}
		var free []string
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			switch strings.ToLower(filepath.Ext(e.Name())) {
			case ".jpg", ".jpeg", ".png", ".webp":
				p := filepath.Join(d, e.Name())
				if !used[p] {
					free = append(free, p)
				}
			}
		}
		if len(free) > 0 {
			sort.Strings(free)
			return free[r.IntN(len(free))], nil
		}
	}
	return "", fmt.Errorf("no free images for gender %q in %s (put .jpg/.png files in %s/%s)", g, dir, dir, g)
}

// fail пишет ошибку в таблицу; статус меняется только для флуда, бана и потери сессии.
func (s *Setup) fail(ctx context.Context, a sheet.Account, cause error) error {
	msg := cause.Error()
	if len(msg) > 300 {
		msg = msg[:300]
	}
	fields := map[string]string{sheet.ColLastError: "profile: " + msg}
	if status, resume := tgclient.Classify(cause); status != sheet.StatusLoginFailed {
		fields[sheet.ColStatus] = status
		if !resume.IsZero() {
			fields[sheet.ColResumeAfter] = resume.UTC().Format(time.RFC3339)
		}
	}
	if err := s.store.Update(ctx, a.Row, fields); err != nil {
		return errors.Join(cause, err)
	}
	return cause
}

func parseSteps(s string) map[string]bool {
	out := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out[p] = true
		}
	}
	return out
}

func joinSteps(done map[string]bool) string {
	var out []string
	for _, st := range allSteps { // фиксированный порядок
		if done[st] {
			out = append(out, st)
		}
	}
	return strings.Join(out, ",")
}

func sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
