package simdata

import "testing"

func TestSameSeedProducesIdenticalData(t *testing.T) {
	a, err := Generate(DefaultConfig(42))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Generate(DefaultConfig(42))
	if a.Hash() != b.Hash() {
		t.Fatalf("aynı tohum farklı veri üretti:\n%s\n%s", a.Hash(), b.Hash())
	}
}

func TestDifferentSeedProducesDifferentData(t *testing.T) {
	a, _ := Generate(DefaultConfig(42))
	b, _ := Generate(DefaultConfig(43))
	if a.Hash() == b.Hash() {
		t.Fatal("farklı tohumlar aynı veriyi üretti")
	}
}

// Altın özet: üretici davranışının istemeden değişmesini yakalar. Üretici bilerek
// değiştirildiğinde bu sabit güncellenir; commit mesajında belirtilmelidir.
// 26 Ekim: ilk kullanıcının e-postası seed.mercan@ddo-case.invalid oldu (ADR-020); özet YALNIZ bu yüzden değişti
// (eski e-postayla yeniden hesaplanınca eski özet birebir çıkıyor: de91b19e…).
func TestGoldenHashForSeed42(t *testing.T) {
	d, _ := Generate(DefaultConfig(42))
	const golden = "7d9f898d3552686c53149f8aa1b9f637210a8b66554df1732ac9fb2ffd26c556"
	if d.Hash() != golden {
		t.Fatalf("seed=42 özeti değişti: %s", d.Hash())
	}
}

func TestScaleMatchesSpec(t *testing.T) {
	d, _ := Generate(DefaultConfig(42))
	if len(d.Exams) != 300 || len(d.Students) != 8000 || len(d.Rooms) != 60 {
		t.Fatalf("ölçek: %d sınav, %d öğrenci, %d salon", len(d.Exams), len(d.Students), len(d.Rooms))
	}
	if len(d.Slots) != 45 {
		t.Fatalf("15 gün x 3 oturum = 45 slot bekleniyordu, %d", len(d.Slots))
	}
	perStudent := map[int64]int{}
	for _, e := range d.Enrollments {
		perStudent[e.StudentID]++
	}
	for id, n := range perStudent {
		if n < 5 || n > 8 {
			t.Fatalf("öğrenci %d: %d ders (5–8 bekleniyordu)", id, n)
		}
	}
	disabled := 0
	for _, s := range d.Students {
		if s.Disabled {
			disabled++
		}
	}
	if disabled < 100 || disabled > 220 { // ~%2 = 160
		t.Fatalf("engelli öğrenci sayısı %d, ~160 bekleniyordu", disabled)
	}
}

func TestRoomsAreConsistent(t *testing.T) {
	d, _ := Generate(DefaultConfig(42))
	active := map[int64]int{}
	accessibleRooms, labs := 0, 0
	acc := map[int64]bool{}
	for _, s := range d.Seats {
		if s.Active {
			active[s.RoomID]++
		}
		if s.Accessible {
			if !s.Active {
				t.Fatalf("erişilebilir koltuk %d aktif olmalı", s.ID)
			}
			acc[s.RoomID] = true
		}
	}
	for _, r := range d.Rooms {
		if r.Capacity != active[r.ID] || r.Capacity <= 0 || r.Capacity > r.Rows*r.Cols {
			t.Fatalf("salon %s kapasite tutarsız: %d (aktif koltuk %d)", r.Code, r.Capacity, active[r.ID])
		}
		if r.Type == "lab" {
			labs++
		}
		if acc[r.ID] {
			accessibleRooms++
		}
	}
	// Erişilebilir koltuklar birbirine komşu olmamalı (aynı dersin iki engelli öğrencisi).
	byRoom := map[int64]map[[2]int]bool{}
	for _, s := range d.Seats {
		if s.Accessible {
			if byRoom[s.RoomID] == nil {
				byRoom[s.RoomID] = map[[2]int]bool{}
			}
			byRoom[s.RoomID][[2]int{s.Row, s.Col}] = true
		}
	}
	for rid, m := range byRoom {
		for k := range m {
			if m[[2]int{k[0], k[1] + 1}] {
				t.Fatalf("salon %d: erişilebilir koltuklar yan yana", rid)
			}
		}
	}
	if labs == 0 || accessibleRooms == 0 {
		t.Fatalf("lab=%d erişilebilir=%d: ikisi de > 0 olmalı", labs, accessibleRooms)
	}
}

func TestDemoAccountsExistForEveryRole(t *testing.T) {
	d, _ := Generate(DefaultConfig(42))
	want := map[string]string{EmailSecretary: "secretary", EmailLecturer: "lecturer",
		EmailInvigilator: "invigilator", EmailStudentAffairs: "student_affairs", EmailStudent: "student"}
	for _, u := range d.Users {
		if role, ok := want[u.Email]; ok {
			if u.Roles[0] != role {
				t.Errorf("%s rolü %v, beklenen %s", u.Email, u.Roles, role)
			}
			delete(want, u.Email)
		}
	}
	if len(want) != 0 {
		t.Fatalf("eksik demo hesapları: %v", want)
	}
	if d.Students[0].UserID != 5 {
		t.Fatal("demo öğrenci hesabı ilk öğrenciye bağlı olmalı")
	}
}

// Yapısal garanti: hiçbir ders için kayıtlı öğrenci sayısı 0 olmamalı, aksi halde
// o sınavın öğrencisi olmaz.
func TestEveryCourseHasStudents(t *testing.T) {
	d, _ := Generate(DefaultConfig(42))
	secCourse := map[int64]int64{}
	for _, s := range d.Sections {
		secCourse[s.ID] = s.CourseID
	}
	cnt := map[int64]int{}
	for _, e := range d.Enrollments {
		cnt[secCourse[e.SectionID]]++
	}
	for _, c := range d.Courses {
		if cnt[c.ID] == 0 {
			t.Fatalf("ders %s için kayıt yok", c.Code)
		}
	}
}

func TestInvalidConfigRejected(t *testing.T) {
	if _, err := Generate(Config{Seed: 1}); err == nil {
		t.Fatal("geçersiz yapılandırma reddedilmeliydi")
	}
}

// Fakülte yapısı (kullanıcı isteği): 4 fakülte, 15 bölüm, her bölümde 1–4. sınıf × 5 ders.
func TestFacultyStructure(t *testing.T) {
	d, _ := Generate(DefaultConfig(42))
	if len(d.Faculties) != 4 || len(d.Departments) != 15 {
		t.Fatalf("%d fakülte, %d bölüm (4 ve 15 bekleniyordu)", len(d.Faculties), len(d.Departments))
	}
	want := map[string][]string{
		"Mühendislik Fakültesi":                {"Yazılım Mühendisliği", "Elektrik Mühendisliği", "Bilgisayar Mühendisliği"},
		"İktisadi ve İdari Bilimler Fakültesi": {"Yönetim Bilişim Sistemleri", "Ekonometri", "Maliye", "Tarih", "Uluslararası İlişkiler"},
		"Sağlık Bilimleri Fakültesi":           {"Hemşirelik", "Eczacılık", "Ebelik", "Hasta Bakım"},
		"Spor Bilimleri Fakültesi":             nil, // 3 bölüm
	}
	byFac := map[int64][]string{}
	for _, dp := range d.Departments {
		byFac[dp.FacultyID] = append(byFac[dp.FacultyID], dp.Name)
	}
	for _, f := range d.Faculties {
		got, exp := byFac[f.ID], want[f.Name]
		if f.Name == "Spor Bilimleri Fakültesi" {
			if len(got) != 3 {
				t.Fatalf("spor bilimleri 3 bölümlü olmalı: %v", got)
			}
			continue
		}
		if len(got) != len(exp) {
			t.Fatalf("%s bölümleri: %v", f.Name, got)
		}
		for i := range exp {
			if got[i] != exp[i] {
				t.Fatalf("%s bölüm %d: %q (beklenen %q)", f.Name, i, got[i], exp[i])
			}
		}
	}
	// her bölümde 1–4. sınıf × 5 ders, kodlar benzersiz
	per := map[[2]int64]int{}
	seen := map[string]bool{}
	for _, c := range d.Courses {
		per[[2]int64{c.DepartmentID, int64(c.Year)}]++
		if seen[c.Code] || c.Name == "" || c.Year < 1 || c.Year > 4 {
			t.Fatalf("ders %q geçersiz/yinelenen (yıl %d)", c.Code, c.Year)
		}
		seen[c.Code] = true
	}
	for dept := int64(1); dept <= 15; dept++ {
		for y := int64(1); y <= 4; y++ {
			if per[[2]int64{dept, y}] != 5 {
				t.Fatalf("bölüm %d sınıf %d: %d ders (5 bekleniyordu)", dept, y, per[[2]int64{dept, y}])
			}
		}
	}
}

func TestLecturersAsRequested(t *testing.T) {
	d, _ := Generate(DefaultConfig(42))
	engNames := []string{"Fahrettin Burak DEMİR", "Ömer ASLAN", "Abdullah Elen", "Buket Toptaş", "Serhat KILIÇARSLAN", "Süleyman Gökhan TAŞKIN", "Mehmet Ali Şimşek", "Murat TOPTAŞ"}
	user := map[int64]User{}
	for _, u := range d.Users {
		user[u.ID] = u
	}
	facOf := map[int64]int64{}
	for _, dp := range d.Departments {
		facOf[dp.ID] = dp.FacultyID
	}
	perFac := map[int64][]string{}
	for _, l := range d.Lecturers {
		perFac[facOf[l.DepartmentID]] = append(perFac[facOf[l.DepartmentID]], user[l.UserID].FullName)
	}
	eng := perFac[d.Faculties[0].ID]
	if len(eng) != 8 {
		t.Fatalf("mühendislik 8 öğretim üyeli olmalı: %v", eng)
	}
	for i, n := range engNames {
		if eng[i] != n {
			t.Fatalf("mühendislik öğretim üyesi %d: %q (beklenen %q)", i, eng[i], n)
		}
	}
	if user[d.Lecturers[0].UserID].Email != EmailLecturer {
		t.Fatal("demo öğretim üyesi hesabı ilk öğretim üyesi olmalı")
	}
	for _, f := range d.Faculties[1:] {
		if n := len(perFac[f.ID]); n < 4 || n > 6 {
			t.Fatalf("%s: %d öğretim üyesi (4–6 bekleniyordu)", f.Name, n)
		}
	}
	// bir ders yalnızca kendi fakültesinin öğretim üyesince verilir
	lecFac := map[int64]int64{}
	for _, l := range d.Lecturers {
		lecFac[l.ID] = facOf[l.DepartmentID]
	}
	courseFac := map[int64]int64{}
	for _, c := range d.Courses {
		courseFac[c.ID] = facOf[c.DepartmentID]
	}
	for _, s := range d.Sections {
		if lecFac[s.LecturerID] != courseFac[s.CourseID] {
			t.Fatalf("şube %d: öğretim üyesi başka fakülteden", s.ID)
		}
	}
}

// "Alttan ders": öğrenci yalnızca kendi bölümünün ve kendi sınıfı veya alt sınıfların derslerini alır.
func TestEnrollmentsStayInOwnDepartmentAndYear(t *testing.T) {
	d, _ := Generate(DefaultConfig(42))
	course := map[int64]Course{}
	for _, c := range d.Courses {
		course[c.ID] = c
	}
	secCourse := map[int64]int64{}
	for _, s := range d.Sections {
		secCourse[s.ID] = s.CourseID
	}
	maxYear := map[int64]int{}
	for _, e := range d.Enrollments {
		c := course[secCourse[e.SectionID]]
		if c.DepartmentID != d.Students[e.StudentID-1].DepartmentID {
			t.Fatalf("öğrenci %d başka bölümün dersini aldı", e.StudentID)
		}
		maxYear[e.StudentID] = max(maxYear[e.StudentID], c.Year)
	}
	if len(maxYear) != len(d.Students) {
		t.Fatalf("dersi olmayan öğrenci var: %d/%d", len(maxYear), len(d.Students))
	}
}

// Demo hesapları anlamlıdır: öğretim üyesi = Fahrettin Burak DEMİR, gözetmen = Ömer ASLAN (aynı zamanda
// öğretim üyesi), öğrenci = Yazılım Mühendisliği 4. sınıf (demo "Prompt Mühendisliği" sınavının öğrencisi).
func TestDemoAccountsAreMeaningful(t *testing.T) {
	d, _ := Generate(DefaultConfig(42))
	byEmail := map[string]User{}
	for _, u := range d.Users {
		byEmail[u.Email] = u
	}
	if byEmail[EmailLecturer].FullName != "Fahrettin Burak DEMİR" || byEmail[EmailInvigilator].FullName != "Ömer ASLAN" {
		t.Fatalf("demo adları: %q / %q", byEmail[EmailLecturer].FullName, byEmail[EmailInvigilator].FullName)
	}
	inv := byEmail[EmailInvigilator]
	if len(inv.Roles) != 2 || inv.Roles[0] != "invigilator" || inv.Roles[1] != "lecturer" {
		t.Fatalf("Ömer ASLAN rolleri: %v", inv.Roles)
	}
	isLecturer := false
	for _, l := range d.Lecturers {
		isLecturer = isLecturer || l.UserID == inv.ID
	}
	if !isLecturer {
		t.Fatal("Ömer ASLAN öğretim üyesi kaydı da olmalı")
	}
	s1 := d.Students[0]
	if s1.DepartmentID != 1 || d.Departments[0].Code != "YZM" {
		t.Fatalf("demo öğrenci Yazılım Mühendisliği öğrencisi olmalı: dept %d", s1.DepartmentID)
	}
	secCourse := map[int64]int64{}
	for _, sec := range d.Sections {
		secCourse[sec.ID] = sec.CourseID
	}
	year := map[int64]int{}
	for _, c := range d.Courses {
		year[c.ID] = c.Year
	}
	maxY := 0
	for _, e := range d.Enrollments {
		if e.StudentID == 1 {
			maxY = max(maxY, year[secCourse[e.SectionID]])
		}
	}
	if maxY != 4 {
		t.Fatalf("demo öğrenci 4. sınıf dersleri almalı (en yüksek sınıf %d)", maxY)
	}
}
