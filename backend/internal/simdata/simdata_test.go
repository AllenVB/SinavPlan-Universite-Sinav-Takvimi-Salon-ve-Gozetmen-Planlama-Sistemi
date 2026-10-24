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
func TestGoldenHashForSeed42(t *testing.T) {
	d, _ := Generate(DefaultConfig(42))
	const golden = "3d33cbcecbaa4db030fee171e9175eec7ec334c1e254060abdf35c6f44d558b2"
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
