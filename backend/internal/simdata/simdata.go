// Package simdata, deterministik demo verisi üretir (proje.md §8): aynı tohum
// her zaman aynı veri kümesini verir. Paket veritabanına dokunmaz; yazma işi
// cmd/simulator'dadır. Rastgelelik yalnızca verilen tohumdan türeyen PCG
// üretecinden gelir, saat veya global rand kullanılmaz.
package simdata

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"time"
)

// Config, üretilecek veri kümesinin boyutunu belirler.
type Config struct {
	Seed     int64
	Exams    int
	Students int
	Rooms    int
	// Departments: kullanılacak bölüm sayısı (1–15, fakülte sırasıyla). 0 = otomatik (sınav sayısına göre).
	Departments    int
	Days           int // sınav dönemi gün sayısı
	SessionsPerDay int
	DisabilityRate float64
}

// DefaultConfig, proje.md'deki ölçeği verir: 300 sınav / 8.000 öğrenci / 60 salon.
func DefaultConfig(seed int64) Config {
	return Config{Seed: seed, Exams: 300, Students: 8000, Rooms: 60, Departments: 15,
		Days: 15, SessionsPerDay: 3, DisabilityRate: 0.02}
}

const (
	numYears       = 4 // her bölümde 1–4. sınıf
	coursesPerYear = 5 // her sınıfta 5 ders (curriculum.go)
)

// usedDepartments, üretimde kullanılacak bölüm sayısı: açıkça verilmişse o, yoksa sınav sayısına
// göre (küçük test veri kümeleri az bölümle çalışır; her bölüm-sınıfta en az 2 ders kalır).
func (c Config) usedDepartments() int {
	n := c.Departments
	if n <= 0 {
		n = c.Exams / 8
	}
	total := 0
	for _, f := range faculties {
		total += len(f.Depts)
	}
	return max(1, min(n, total))
}

// Demo hesapları (parola DEMO_PASSWORD env'inden gelir).
const (
	EmailSecretary      = "seed.mercan@ddo-case.invalid"
	EmailLecturer       = "hoca@sinavplan.test"
	EmailInvigilator    = "gozetmen@sinavplan.test"
	EmailStudentAffairs = "ogrenciisleri@sinavplan.test"
	EmailStudent        = "ogrenci@sinavplan.test"
)

type Faculty struct {
	ID         int64
	Code, Name string
}

type Department struct {
	ID, FacultyID int64
	Code, Name    string
}

type User struct {
	ID       int64
	Email    string
	FullName string
	Roles    []string
}

type Lecturer struct{ ID, UserID, DepartmentID int64 }

type Course struct {
	ID, DepartmentID int64
	Code, Name       string
	Year             int
	RequiresLab      bool
}

type Section struct {
	ID, CourseID, LecturerID int64
	Code                     string
}

type Student struct {
	ID, DepartmentID int64
	UserID           int64 // 0 = hesabı yok
	No, FullName     string
	Disabled         bool
}

type Enrollment struct{ StudentID, SectionID int64 }

type Room struct {
	ID                   int64
	Code, Building, Type string
	Floor, Rows, Cols    int
	Capacity             int
}

type Seat struct {
	ID, RoomID         int64
	Row, Col           int
	Active, Accessible bool
}

type Period struct {
	ID                   int64
	Name, Kind           string
	StartDate            time.Time
	Days, SessionsPerDay int
}

type Slot struct {
	ID, PeriodID     int64
	Day, Session     int
	StartsAt, EndsAt time.Time
}

type Exam struct {
	ID, PeriodID, CourseID int64
	DurationMinutes        int
	RequiresLab            bool
}

// Dataset, üretilen tüm tabloları tutar. Kimlikler 1'den başlayarak ardışıktır.
type Dataset struct {
	Config      Config
	Faculties   []Faculty
	Departments []Department
	Users       []User
	Lecturers   []Lecturer
	Courses     []Course
	Sections    []Section
	Students    []Student
	Enrollments []Enrollment
	Rooms       []Room
	Seats       []Seat
	Period      Period
	Slots       []Slot
	Exams       []Exam
	// RoomBlocks: salonun belirli bir oturumda kapalı olduğu (bakım, başka etkinlik) kayıtlar; üretilen veride boştur ve
	// Hash'e girmez (veri tabanından yüklenen gerçek dönemde dolar).
	RoomBlocks []RoomBlock
}

// RoomBlock, bir salonun bir slotta kullanıma kapalı olduğunu söyler.
type RoomBlock struct{ RoomID, SlotID int64 }

var (
	firstNames = []string{"Ahmet", "Mehmet", "Ayşe", "Fatma", "Mustafa", "Zeynep", "Elif", "Emre", "Burak", "Selin",
		"Can", "Deniz", "Ece", "Furkan", "Gizem", "Hakan", "İrem", "Kerem", "Merve", "Onur",
		"Pınar", "Rabia", "Serkan", "Tuğba", "Umut", "Yasin", "Zehra", "Berk", "Cansu", "Ege"}
	lastNames = []string{"Yılmaz", "Kaya", "Demir", "Şahin", "Çelik", "Yıldız", "Yıldırım", "Öztürk", "Aydın", "Özdemir",
		"Arslan", "Doğan", "Kılıç", "Aslan", "Çetin", "Kara", "Koç", "Kurt", "Özkan", "Şimşek",
		"Polat", "Korkmaz", "Erdoğan", "Güneş", "Aksoy", "Tekin", "Bozkurt", "Acar", "Yavuz", "Uçar"}
	classicLayouts = [][2]int{{5, 8}, {6, 8}, {6, 10}, {8, 10}, {8, 12}, {10, 12}, {10, 14}}
	labLayouts     = [][2]int{{4, 8}, {5, 8}, {5, 10}, {6, 10}}
	durations      = []int{60, 75, 90, 120}
	sessionStart   = [][2]int{{9, 0}, {13, 0}, {16, 30}} // saat, dakika
)

// Generate, yapılandırmaya göre veri kümesini üretir.
func Generate(cfg Config) (*Dataset, error) {
	if cfg.Exams < cfg.usedDepartments()*numYears || cfg.Students <= 0 || cfg.Rooms < 2 ||
		cfg.Days <= 0 || cfg.SessionsPerDay <= 0 || cfg.SessionsPerDay > len(sessionStart) {
		return nil, fmt.Errorf("geçersiz yapılandırma: %+v", cfg)
	}
	r := rand.New(rand.NewPCG(uint64(cfg.Seed), 0x9e3779b97f4a7c15))
	d := &Dataset{Config: cfg}

	d.genDepartments()
	d.genUsersAndLecturers(r)
	d.genCourses(r)
	d.genSections(r)
	d.genStudentsAndEnrollments(r)
	d.genRooms(r)
	d.genPeriod(r)
	return d, nil
}

// genDepartments: kullanılan bölümler fakülte sırasıyla; fakülte kimlikleri yalnızca kullanılan
// fakültelere verilir (1..).
func (d *Dataset) genDepartments() {
	defs, facOf := allDepartments()
	n := d.Config.usedDepartments()
	facID := map[int]int64{}
	for i := 0; i < n; i++ {
		fi := facOf[i]
		if _, ok := facID[fi]; !ok {
			facID[fi] = int64(len(d.Faculties) + 1)
			d.Faculties = append(d.Faculties, Faculty{ID: facID[fi], Code: faculties[fi].Code, Name: faculties[fi].Name})
		}
		d.Departments = append(d.Departments, Department{ID: int64(i + 1), FacultyID: facID[fi], Code: defs[i].Code, Name: defs[i].Name})
	}
}

func name(r *rand.Rand) string {
	return firstNames[r.IntN(len(firstNames))] + " " + lastNames[r.IntN(len(lastNames))]
}

func (d *Dataset) addUser(email, full string, roles ...string) int64 {
	id := int64(len(d.Users) + 1)
	d.Users = append(d.Users, User{ID: id, Email: email, FullName: full, Roles: roles})
	return id
}

// Kullanıcı sırası: 1..5 demo hesapları, sonra öğretim üyeleri (fakülte sırasıyla), sonra yalnız-gözetmenler.
// Mühendislik Fakültesi'nin 8 öğretim üyesi verilen adlardır (ilki demo öğretim üyesi hesabıdır);
// diğer fakültelerin 5–6 öğretim üyesi rastgele adlıdır. Öğretim üyeleri fakülte düzeyindedir: bir
// fakültenin tüm bölümlerinin derslerini verebilir; DepartmentID "ana bölümü"dür (dönüşümlü).
func (d *Dataset) genUsersAndLecturers(r *rand.Rand) {
	d.addUser(EmailSecretary, "Demo Sekreter", "secretary")
	lecUser := d.addUser(EmailLecturer, "Demo Öğretim Üyesi", "lecturer")
	invUser := d.addUser(EmailInvigilator, "Demo Gözetmen", "invigilator")
	d.addUser(EmailStudentAffairs, "Demo Öğrenci İşleri", "student_affairs")
	d.addUser(EmailStudent, "Demo Öğrenci", "student")

	facIdx := map[string]int{}
	for fi, f := range faculties {
		facIdx[f.Code] = fi
	}
	i := 0
	for _, fac := range d.Faculties {
		def := faculties[facIdx[fac.Code]]
		var depts []int64
		for _, dp := range d.Departments {
			if dp.FacultyID == fac.ID {
				depts = append(depts, dp.ID)
			}
		}
		names := def.Lecturers
		if len(names) == 0 {
			for k := 0; k < def.LecturerCount; k++ {
				names = append(names, name(r))
			}
		}
		for k, nm := range names {
			i++
			uid := lecUser
			switch {
			case i == 1:
				d.Users[lecUser-1].FullName = nm // demo öğretim üyesi hesabı ilk öğretim üyesidir
			case i == 2:
				// demo gözetmen hesabı ikinci öğretim üyesidir (Ömer ASLAN): hem öğretim üyesi hem gözetmen
				uid = invUser
				d.Users[invUser-1].FullName = nm
				d.Users[invUser-1].Roles = []string{"invigilator", "lecturer"}
			default:
				// İlk 20 öğretim üyesi aynı zamanda gözetmenlik yapabilir (V-4).
				roles := []string{"lecturer"}
				if i <= 20 {
					roles = append(roles, "invigilator")
				}
				uid = d.addUser(fmt.Sprintf("hoca%02d@sinavplan.test", i), nm, roles...)
			}
			d.Lecturers = append(d.Lecturers, Lecturer{ID: int64(i), UserID: uid, DepartmentID: depts[k%len(depts)]})
		}
	}
	for k := 1; k <= 80; k++ {
		d.addUser(fmt.Sprintf("gozetmen%02d@sinavplan.test", k), name(r), "invigilator")
	}
}

// genCourses: müfredat yapısı bölüm × sınıf (1–4) gruplarıdır; her grupta coursesPerYear ders.
// Öğrenciler dersleri kendi grubundan (ve bazıları alttan) alır; bu, çatışma grafını gerçekçi ve
// renklendirilebilir kılar (tamamen rastgele seçimde her ders çifti çatışır).
func (d *Dataset) genCourses(r *rand.Rand) {
	defs, facOf := allDepartments()
	perYear := min(coursesPerYear, d.Config.Exams/(len(d.Departments)*numYears))
	id := int64(0)
	for di := range d.Departments {
		for y := 1; y <= numYears; y++ {
			for k := 1; k <= perYear; k++ {
				id++
				d.Courses = append(d.Courses, Course{ID: id, DepartmentID: int64(di + 1),
					Code: fmt.Sprintf("%s%d%02d", defs[di].Code, y, k), Name: defs[di].Courses[y-1][k-1],
					Year: y, RequiresLab: r.Float64() < faculties[facOf[di]].LabRate})
			}
		}
	}
}

func (d *Dataset) genSections(r *rand.Rand) {
	facOfDept := map[int64]int64{}
	for _, dp := range d.Departments {
		facOfDept[dp.ID] = dp.FacultyID
	}
	byFac := map[int64][]int64{}
	for _, l := range d.Lecturers {
		byFac[facOfDept[l.DepartmentID]] = append(byFac[facOfDept[l.DepartmentID]], l.ID)
	}
	id := int64(0)
	for _, c := range d.Courses {
		pool := byFac[facOfDept[c.DepartmentID]]
		n := 2
		if r.Float64() < 0.3 {
			n = 3
		}
		for s := 0; s < n; s++ {
			id++
			d.Sections = append(d.Sections, Section{ID: id, CourseID: c.ID,
				Code: string(rune('A' + s)), LecturerID: pool[r.IntN(len(pool))]})
		}
	}
}

// genStudentsAndEnrollments: her öğrenci bir bölüm ve sınıfa aittir; o grubun TÜM derslerini alır.
// 2. sınıf ve üstündeki öğrencilerin ~%30'u ayrıca aynı bölümün alt sınıflarından 1–2 ders "alttan" alır.
func (d *Dataset) genStudentsAndEnrollments(r *rand.Rand) {
	groupCourses := map[[2]int][]int64{} // (bölüm, sınıf) -> ders id'leri
	sectionsOf := map[int64][]int64{}
	for _, c := range d.Courses {
		key := [2]int{int(c.DepartmentID), c.Year}
		groupCourses[key] = append(groupCourses[key], c.ID)
	}
	for _, s := range d.Sections {
		sectionsOf[s.CourseID] = append(sectionsOf[s.CourseID], s.ID)
	}
	nd := len(d.Departments)

	for i := 1; i <= d.Config.Students; i++ {
		dept := r.IntN(nd) + 1
		year := r.IntN(numYears) + 1
		if i == 1 { // demo öğrenci: Yazılım Mühendisliği 4. sınıf (demo sınavın öğrencisi)
			dept, year = 1, numYears
		}
		st := Student{ID: int64(i), DepartmentID: int64(dept), No: fmt.Sprintf("2026%05d", i),
			FullName: name(r), Disabled: r.Float64() < d.Config.DisabilityRate}
		if i == 1 {
			st.UserID = 5 // demo öğrenci hesabı
			st.FullName = "Demo Öğrenci"
		}
		d.Students = append(d.Students, st)

		chosen := append([]int64(nil), groupCourses[[2]int{dept, year}]...)
		if year > 1 && r.Float64() < 0.3 {
			var lower []int64
			for y := 1; y < year; y++ {
				lower = append(lower, groupCourses[[2]int{dept, y}]...)
			}
			for _, idx := range r.Perm(len(lower))[:min(1+r.IntN(2), len(lower))] {
				chosen = append(chosen, lower[idx])
			}
		}
		for _, cid := range chosen {
			secs := sectionsOf[cid]
			d.Enrollments = append(d.Enrollments, Enrollment{StudentID: int64(i), SectionID: secs[r.IntN(len(secs))]})
		}
	}
}

func (d *Dataset) genRooms(r *rand.Rand) {
	labs := d.Config.Rooms / 5
	seatID := int64(0)
	for i := 1; i <= d.Config.Rooms; i++ {
		isLab := i > d.Config.Rooms-labs
		layouts, typ, prefix := classicLayouts, "classic", "D"
		if isLab {
			layouts, typ, prefix = labLayouts, "lab", "LAB"
		}
		lay := layouts[r.IntN(len(layouts))]
		room := Room{ID: int64(i), Code: fmt.Sprintf("%s-%02d", prefix, i), Building: string(rune('A' + (i-1)%4)),
			Floor: (i - 1) % 4, Type: typ, Rows: lay[0], Cols: lay[1]}

		// Erişilebilir koltuklar ön sırada, kapıya yakın ve BİRBİRİNE KOMŞU DEĞİL
		// (sütun 0, 2, 4): aynı dersin iki engelli öğrencisi yan yana oturamayacağı için
		// (T-3) bitişik erişilebilir koltuklar bir salona dersin tek engelli öğrencisini
		// sığdırırdı. Salonların ~2/3'ünde var; laboratuvarların yarısında da var.
		hasAccessible := (!isLab && i%3 != 0) || (isLab && i%2 == 0)
		for row := 0; row < room.Rows; row++ {
			for col := 0; col < room.Cols; col++ {
				seatID++
				accessible := hasAccessible && row == 0 && col%2 == 0 && col <= 4
				active := accessible || r.Float64() >= 0.04 // koltukların ~%4'ü kullanılamaz
				if active {
					room.Capacity++
				}
				d.Seats = append(d.Seats, Seat{ID: seatID, RoomID: room.ID, Row: row, Col: col,
					Active: active, Accessible: accessible})
			}
		}
		d.Rooms = append(d.Rooms, room)
	}
}

// genPeriod: dönem, slotlar ve sınavlar. Gün sırası yalnızca iş günlerini sayar
// (hafta sonları atlanır); day_index ardışıklığı T-1'deki "ardışık gün" ölçütüdür.
func (d *Dataset) genPeriod(r *rand.Rand) {
	start := time.Date(2027, 1, 11, 0, 0, 0, 0, time.UTC) // Pazartesi
	d.Period = Period{ID: 1, Name: "2026-2027 Güz Final", Kind: "final", StartDate: start,
		Days: d.Config.Days, SessionsPerDay: d.Config.SessionsPerDay}

	day := start
	id := int64(0)
	const trt = 3 * time.Hour // Europe/Istanbul, yıl boyu UTC+3
	for di := 0; di < d.Config.Days; di++ {
		for day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			day = day.AddDate(0, 0, 1)
		}
		for s := 0; s < d.Config.SessionsPerDay; s++ {
			id++
			st := day.Add(time.Duration(sessionStart[s][0])*time.Hour +
				time.Duration(sessionStart[s][1])*time.Minute - trt)
			d.Slots = append(d.Slots, Slot{ID: id, PeriodID: 1, Day: di, Session: s,
				StartsAt: st, EndsAt: st.Add(150 * time.Minute)})
		}
		day = day.AddDate(0, 0, 1)
	}

	for i, c := range d.Courses {
		d.Exams = append(d.Exams, Exam{ID: int64(i + 1), PeriodID: 1, CourseID: c.ID,
			DurationMinutes: durations[r.IntN(len(durations))], RequiresLab: c.RequiresLab})
	}
}

// Hash, veri kümesinin kanonik özetidir; "aynı tohum = aynı veri" testi bunu karşılaştırır.
func (d *Dataset) Hash() string {
	h := sha256.New()
	w := func(label string, n int, f func(i int) any) {
		fmt.Fprintf(h, "#%s %d\n", label, n)
		for i := 0; i < n; i++ {
			fmt.Fprintf(h, "%+v\n", f(i))
		}
	}
	w("faculties", len(d.Faculties), func(i int) any { return d.Faculties[i] })
	w("departments", len(d.Departments), func(i int) any { return d.Departments[i] })
	w("users", len(d.Users), func(i int) any { return d.Users[i] })
	w("lecturers", len(d.Lecturers), func(i int) any { return d.Lecturers[i] })
	w("courses", len(d.Courses), func(i int) any { return d.Courses[i] })
	w("sections", len(d.Sections), func(i int) any { return d.Sections[i] })
	w("students", len(d.Students), func(i int) any { return d.Students[i] })
	w("enrollments", len(d.Enrollments), func(i int) any { return d.Enrollments[i] })
	w("rooms", len(d.Rooms), func(i int) any { return d.Rooms[i] })
	w("seats", len(d.Seats), func(i int) any { return d.Seats[i] })
	w("period", 1, func(int) any { return d.Period })
	w("slots", len(d.Slots), func(i int) any { return d.Slots[i] })
	w("exams", len(d.Exams), func(i int) any { return d.Exams[i] })
	return hex.EncodeToString(h.Sum(nil))
}
