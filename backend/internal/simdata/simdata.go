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
	Seed           int64
	Exams          int
	Students       int
	Rooms          int
	Days           int // sınav dönemi gün sayısı
	SessionsPerDay int
	DisabilityRate float64
}

// DefaultConfig, proje.md'deki ölçeği verir: 300 sınav / 8.000 öğrenci / 60 salon.
func DefaultConfig(seed int64) Config {
	return Config{Seed: seed, Exams: 300, Students: 8000, Rooms: 60,
		Days: 15, SessionsPerDay: 3, DisabilityRate: 0.02}
}

const (
	numDepartments = 6 // öğrencilerin bağlı olduğu bölümler; ayrıca "ORT" ortak dersler bölümü eklenir
	numYears       = 4
)

// Demo hesapları (parola DEMO_PASSWORD env'inden gelir).
const (
	EmailSecretary      = "sekreter@sinavplan.test"
	EmailLecturer       = "hoca@sinavplan.test"
	EmailInvigilator    = "gozetmen@sinavplan.test"
	EmailStudentAffairs = "ogrenciisleri@sinavplan.test"
	EmailStudent        = "ogrenci@sinavplan.test"
)

type Department struct {
	ID         int64
	Code, Name string
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
}

var (
	firstNames = []string{"Ahmet", "Mehmet", "Ayşe", "Fatma", "Mustafa", "Zeynep", "Elif", "Emre", "Burak", "Selin",
		"Can", "Deniz", "Ece", "Furkan", "Gizem", "Hakan", "İrem", "Kerem", "Merve", "Onur",
		"Pınar", "Rabia", "Serkan", "Tuğba", "Umut", "Yasin", "Zehra", "Berk", "Cansu", "Ege"}
	lastNames = []string{"Yılmaz", "Kaya", "Demir", "Şahin", "Çelik", "Yıldız", "Yıldırım", "Öztürk", "Aydın", "Özdemir",
		"Arslan", "Doğan", "Kılıç", "Aslan", "Çetin", "Kara", "Koç", "Kurt", "Özkan", "Şimşek",
		"Polat", "Korkmaz", "Erdoğan", "Güneş", "Aksoy", "Tekin", "Bozkurt", "Acar", "Yavuz", "Uçar"}
	deptNames = [numDepartments][2]string{
		{"BLM", "Bilgisayar Mühendisliği"}, {"ELK", "Elektrik-Elektronik Mühendisliği"},
		{"MAK", "Makine Mühendisliği"}, {"INS", "İnşaat Mühendisliği"},
		{"END", "Endüstri Mühendisliği"}, {"MAT", "Matematik"},
	}
	classicLayouts = [][2]int{{5, 8}, {6, 8}, {6, 10}, {8, 10}, {8, 12}, {10, 12}, {10, 14}}
	labLayouts     = [][2]int{{4, 8}, {5, 8}, {5, 10}, {6, 10}}
	durations      = []int{60, 75, 90, 120}
	sessionStart   = [][2]int{{9, 0}, {13, 0}, {16, 30}} // saat, dakika
)

// Generate, yapılandırmaya göre veri kümesini üretir.
func Generate(cfg Config) (*Dataset, error) {
	if cfg.Exams < numDepartments*numYears || cfg.Students <= 0 || cfg.Rooms < 2 ||
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

func (d *Dataset) genDepartments() {
	for i, n := range deptNames {
		d.Departments = append(d.Departments, Department{ID: int64(i + 1), Code: n[0], Name: n[1]})
	}
	d.Departments = append(d.Departments, Department{ID: numDepartments + 1, Code: "ORT", Name: "Ortak Dersler"})
}

func name(r *rand.Rand) string {
	return firstNames[r.IntN(len(firstNames))] + " " + lastNames[r.IntN(len(lastNames))]
}

func (d *Dataset) addUser(email, full string, roles ...string) int64 {
	id := int64(len(d.Users) + 1)
	d.Users = append(d.Users, User{ID: id, Email: email, FullName: full, Roles: roles})
	return id
}

// Kullanıcı sırası: 1..5 demo hesapları, sonra öğretim üyeleri, sonra yalnız-gözetmenler.
func (d *Dataset) genUsersAndLecturers(r *rand.Rand) {
	d.addUser(EmailSecretary, "Demo Sekreter", "secretary")
	lecUser := d.addUser(EmailLecturer, "Demo Öğretim Üyesi", "lecturer")
	d.addUser(EmailInvigilator, "Demo Gözetmen", "invigilator")
	d.addUser(EmailStudentAffairs, "Demo Öğrenci İşleri", "student_affairs")
	d.addUser(EmailStudent, "Demo Öğrenci", "student")

	const numLecturers = 60
	for i := 1; i <= numLecturers; i++ {
		uid := lecUser
		if i > 1 {
			// İlk 20 öğretim üyesi aynı zamanda gözetmenlik yapabilir (V-4).
			roles := []string{"lecturer"}
			if i <= 20 {
				roles = append(roles, "invigilator")
			}
			uid = d.addUser(fmt.Sprintf("hoca%02d@sinavplan.test", i), name(r), roles...)
		}
		dept := int64((i-1)%numDepartments + 1)
		d.Lecturers = append(d.Lecturers, Lecturer{ID: int64(i), UserID: uid, DepartmentID: dept})
	}
	for i := 1; i <= 80; i++ {
		d.addUser(fmt.Sprintf("gozetmen%02d@sinavplan.test", i), name(r), "invigilator")
	}
}

// genCourses: müfredat yapısı bölüm×yıl gruplarıdır. Öğrenciler dersleri kendi
// bölüm-yıl grubundan seçer; bu, çatışma grafını gerçekçi ve renklendirilebilir
// kılar (tamamen rastgele seçimde her ders çifti çatışır).
func (d *Dataset) genCourses(r *rand.Rand) {
	electives := d.Config.Exams / 25
	perYear := (d.Config.Exams - electives) / (numDepartments * numYears)
	electives = d.Config.Exams - perYear*numDepartments*numYears

	id := int64(0)
	for di := 0; di < numDepartments; di++ {
		for y := 1; y <= numYears; y++ {
			for k := 1; k <= perYear; k++ {
				id++
				code := fmt.Sprintf("%s%d%02d", deptNames[di][0], y, k)
				d.Courses = append(d.Courses, Course{ID: id, DepartmentID: int64(di + 1),
					Code: code, Name: fmt.Sprintf("%s Dersi %d", deptNames[di][1], y*100+k),
					Year: y, RequiresLab: r.Float64() < 0.15})
			}
		}
	}
	for k := 1; k <= electives; k++ {
		id++
		d.Courses = append(d.Courses, Course{ID: id, DepartmentID: numDepartments + 1,
			Code: fmt.Sprintf("ORT3%02d", k), Name: fmt.Sprintf("Seçmeli Ders %d", k), Year: 3})
	}
}

func (d *Dataset) genSections(r *rand.Rand) {
	byDept := map[int64][]int64{}
	for _, l := range d.Lecturers {
		byDept[l.DepartmentID] = append(byDept[l.DepartmentID], l.ID)
	}
	id := int64(0)
	for _, c := range d.Courses {
		pool := byDept[c.DepartmentID]
		if len(pool) == 0 { // ortak dersler: herhangi bir öğretim üyesi
			pool = make([]int64, len(d.Lecturers))
			for i, l := range d.Lecturers {
				pool[i] = l.ID
			}
		}
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

func (d *Dataset) genStudentsAndEnrollments(r *rand.Rand) {
	// Ders ve şube dizinleri
	groupCourses := map[[2]int][]int64{} // (bölüm, yıl) -> ders id'leri
	var electiveIDs []int64
	sectionsOf := map[int64][]int64{}
	for _, c := range d.Courses {
		if c.DepartmentID == numDepartments+1 {
			electiveIDs = append(electiveIDs, c.ID)
		} else {
			key := [2]int{int(c.DepartmentID), c.Year}
			groupCourses[key] = append(groupCourses[key], c.ID)
		}
	}
	for _, s := range d.Sections {
		sectionsOf[s.CourseID] = append(sectionsOf[s.CourseID], s.ID)
	}

	for i := 1; i <= d.Config.Students; i++ {
		dept := r.IntN(numDepartments) + 1
		year := r.IntN(numYears) + 1
		st := Student{ID: int64(i), DepartmentID: int64(dept), No: fmt.Sprintf("2026%05d", i),
			FullName: name(r), Disabled: r.Float64() < d.Config.DisabilityRate}
		if i == 1 {
			st.UserID = 5 // demo öğrenci hesabı
			st.FullName = "Demo Öğrenci"
		}
		d.Students = append(d.Students, st)

		pool := groupCourses[[2]int{dept, year}]
		k := 5 + r.IntN(4) // 5..8 ders
		chosen := make([]int64, 0, k)
		if len(electiveIDs) > 0 && r.Float64() < 0.3 {
			chosen = append(chosen, electiveIDs[r.IntN(len(electiveIDs))])
		}
		for _, idx := range r.Perm(len(pool)) {
			if len(chosen) >= k {
				break
			}
			chosen = append(chosen, pool[idx])
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

		// Erişilebilir koltuklu salonlar: ilk sıra, kapıya yakın iki sütun. Salonların
		// yaklaşık üçte ikisinde var.
		hasAccessible := !isLab && i%3 != 0
		for row := 0; row < room.Rows; row++ {
			for col := 0; col < room.Cols; col++ {
				seatID++
				accessible := hasAccessible && row == 0 && col < 2
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
