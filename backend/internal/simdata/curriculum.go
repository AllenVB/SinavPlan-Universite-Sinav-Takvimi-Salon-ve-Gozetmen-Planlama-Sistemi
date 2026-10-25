package simdata

// Fakülte → bölüm → sınıf yapısı ve ders adları. Gerçekçi, anlaşılır bir müfredat: her
// bölümün 1–4. sınıflarında 5'er ders vardır (4 × 5 × 15 bölüm = 300 sınav).

type facultyDef struct {
	Code, Name string
	Depts      []deptDef
	// Lecturers: verilmişse bu adlar kullanılır; yoksa LecturerCount kadar rastgele ad üretilir.
	Lecturers     []string
	LecturerCount int
	LabRate       float64 // laboratuvar gerektiren ders oranı
}

type deptDef struct {
	Code, Name string
	Courses    [4][5]string // [sınıf-1][5 ders]
}

var faculties = []facultyDef{
	{Code: "MUH", Name: "Mühendislik Fakültesi", LabRate: 0.2,
		Lecturers: []string{"Fahrettin Burak DEMİR", "Ömer ASLAN", "Abdullah Elen", "Buket Toptaş", "Serhat KILIÇARSLAN",
			"Süleyman Gökhan TAŞKIN", "Mehmet Ali Şimşek", "Murat TOPTAŞ"},
		Depts: []deptDef{
			{"YZM", "Yazılım Mühendisliği", [4][5]string{
				{"Programlamaya Giriş", "Matematik I", "Fizik I", "Yazılım Mühendisliğine Giriş", "Ayrık Matematik"},
				{"Veri Yapıları ve Algoritmalar", "Nesne Yönelimli Programlama", "Veritabanı Sistemleri", "Sayısal Mantık Tasarımı", "Olasılık ve İstatistik"},
				{"Yazılım Mimarisi", "Yazılım Test ve Kalite", "İşletim Sistemleri", "Web Programlama", "Mobil Uygulama Geliştirme"},
				{"Yazılım Proje Yönetimi", "Yapay Zekâ", "Bulut Bilişim", "Bilgi Güvenliği", "Bitirme Projesi"}}},
			{"ELK", "Elektrik Mühendisliği", [4][5]string{
				{"Devre Analizi I", "Matematik I", "Fizik I", "Elektrik Mühendisliğine Giriş", "Programlama Temelleri"},
				{"Devre Analizi II", "Elektronik I", "Sinyaller ve Sistemler", "Elektromanyetik Alan Teorisi", "Diferansiyel Denklemler"},
				{"Elektronik II", "Kontrol Sistemleri", "Güç Sistemleri", "Mikroişlemciler", "Haberleşme Sistemleri"},
				{"Elektrik Makineleri", "Güç Elektroniği", "Sayısal Sinyal İşleme", "Yüksek Gerilim Tekniği", "Bitirme Projesi"}}},
			{"BLM", "Bilgisayar Mühendisliği", [4][5]string{
				{"Bilgisayar Programlama I", "Matematik I", "Fizik I", "Bilgisayar Mühendisliğine Giriş", "Lineer Cebir"},
				{"Veri Yapıları", "Bilgisayar Organizasyonu", "Mantık Devreleri", "Algoritma Analizi", "Ayrık Matematik"},
				{"İşletim Sistemleri", "Bilgisayar Ağları", "Veritabanı Yönetim Sistemleri", "Yazılım Mühendisliği", "Biçimsel Diller ve Otomata"},
				{"Yapay Zekâ", "Bilgisayar Grafikleri", "Dağıtık Sistemler", "Siber Güvenlik", "Bitirme Projesi"}}},
		}},
	{Code: "IIBF", Name: "İktisadi ve İdari Bilimler Fakültesi", LabRate: 0.05, LecturerCount: 6,
		Depts: []deptDef{
			{"YBS", "Yönetim Bilişim Sistemleri", [4][5]string{
				{"İşletmeye Giriş", "Genel Muhasebe", "Matematik I", "Bilgisayar Programlamaya Giriş", "Mikroekonomi"},
				{"Yönetim Bilişim Sistemleri", "Veritabanı Yönetimi", "İstatistik I", "Sistem Analizi ve Tasarımı", "Makroekonomi"},
				{"Kurumsal Kaynak Planlama", "Veri Madenciliği", "Web Tabanlı Uygulamalar", "İş Zekâsı", "Proje Yönetimi"},
				{"Bilişim Hukuku", "Elektronik Ticaret", "Bilgi Güvenliği Yönetimi", "Bilişim Stratejileri", "Bitirme Çalışması"}}},
			{"EKM", "Ekonometri", [4][5]string{
				{"Mikroekonomi I", "Matematik I", "İstatistik I", "Genel Muhasebe", "Hukukun Temel Kavramları"},
				{"Makroekonomi I", "Matematiksel İktisat", "İstatistik II", "Ekonometri I", "Para ve Banka"},
				{"Ekonometri II", "Zaman Serisi Analizi", "Uluslararası İktisat", "Yöneylem Araştırması", "Finansal Ekonometri"},
				{"Panel Veri Analizi", "Ekonomik Büyüme", "Tahmin ve Öngörü", "Uygulamalı Ekonometri", "Bitirme Çalışması"}}},
			{"MLY", "Maliye", [4][5]string{
				{"Maliyeye Giriş", "İktisada Giriş", "Genel Muhasebe", "Hukukun Temel Kavramları", "Matematik"},
				{"Kamu Maliyesi", "Vergi Hukuku", "Mikroekonomi", "Maliye Politikası", "İstatistik"},
				{"Vergi Teorisi", "Bütçe ve Mali Planlama", "Mali Hukuk", "Devlet Borçları", "Uluslararası Maliye"},
				{"Mahalli İdareler Maliyesi", "Vergi Uygulamaları", "Kamu Harcamaları Analizi", "Sosyal Güvenlik Maliyesi", "Bitirme Çalışması"}}},
			{"TRH", "Tarih", [4][5]string{
				{"Tarih Biliminin Temelleri", "İslam Öncesi Türk Tarihi", "Osmanlı Türkçesi I", "Eski Çağ Tarihi", "Atatürk İlkeleri ve İnkılap Tarihi I"},
				{"Orta Çağ Tarihi", "Osmanlı Tarihi I", "Osmanlı Türkçesi II", "Tarih Metodolojisi", "Türk-İslam Devletleri Tarihi"},
				{"Osmanlı Tarihi II", "Yeni Çağ Avrupa Tarihi", "Türk Kültür Tarihi", "Tarihî Coğrafya", "Arşivcilik"},
				{"Yakın Çağ Tarihi", "Çağdaş Türk Tarihi", "Tarih Felsefesi", "Türk Diplomasi Tarihi", "Bitirme Çalışması"}}},
			{"UIL", "Uluslararası İlişkiler", [4][5]string{
				{"Uluslararası İlişkilere Giriş", "Siyaset Bilimine Giriş", "Hukukun Temel Kavramları", "İktisada Giriş", "Tarih ve Dünya Siyaseti"},
				{"Uluslararası İlişkiler Kuramları", "Uluslararası Hukuk I", "Karşılaştırmalı Siyaset", "Diplomasi Tarihi", "Siyasi Düşünceler Tarihi"},
				{"Dış Politika Analizi", "Uluslararası Örgütler", "Uluslararası Politik Ekonomi", "Avrupa Birliği", "Türk Dış Politikası"},
				{"Güvenlik Çalışmaları", "Ortadoğu Siyaseti", "Küresel Sorunlar", "Müzakere ve Arabuluculuk", "Bitirme Çalışması"}}},
		}},
	{Code: "SBF", Name: "Sağlık Bilimleri Fakültesi", LabRate: 0.15, LecturerCount: 6,
		Depts: []deptDef{
			{"HEM", "Hemşirelik", [4][5]string{
				{"Anatomi", "Fizyoloji", "Hemşirelik Esasları", "Mikrobiyoloji", "Hemşirelikte Temel Kavramlar"},
				{"İç Hastalıkları Hemşireliği", "Farmakoloji", "Patoloji", "Cerrahi Hastalıklar Hemşireliği", "Sağlık Değerlendirmesi"},
				{"Doğum ve Kadın Sağlığı Hemşireliği", "Çocuk Sağlığı ve Hastalıkları Hemşireliği", "Ruh Sağlığı Hemşireliği", "Halk Sağlığı Hemşireliği", "Hemşirelikte Araştırma"},
				{"Yoğun Bakım Hemşireliği", "Hemşirelikte Yönetim", "Acil Hemşireliği", "Geriatri Hemşireliği", "İntörn Klinik Uygulama"}}},
			{"ECZ", "Eczacılık", [4][5]string{
				{"Genel Kimya", "Anatomi ve Fizyoloji", "Eczacılığa Giriş", "Biyoloji", "Eczacılık Matematiği"},
				{"Organik Kimya", "Analitik Kimya", "Farmasötik Botanik", "Biyokimya", "Mikrobiyoloji"},
				{"Farmakoloji I", "Farmasötik Teknoloji I", "Farmakognozi", "Farmasötik Kimya I", "Toksikoloji"},
				{"Klinik Eczacılık", "Farmasötik Teknoloji II", "Farmakoloji II", "Biyofarmasötik", "Eczacılık Uygulaması"}}},
			{"EBE", "Ebelik", [4][5]string{
				{"Anatomi", "Fizyoloji", "Ebeliğe Giriş", "Mikrobiyoloji", "Sağlık Psikolojisi"},
				{"Doğum Bilgisi", "Kadın Hastalıkları", "Farmakoloji", "Beslenme İlkeleri", "Temel Ebelik Uygulamaları"},
				{"Doğum ve Doğum Sonu Bakım", "Yenidoğan Sağlığı", "Jinekoloji ve Üreme Sağlığı", "Halk Sağlığı", "Ebelikte Araştırma"},
				{"Riskli Gebelikler", "Doğum Eylemi Yönetimi", "Ebelikte Danışmanlık", "Aile Planlaması", "Klinik Uygulama"}}},
			{"HBK", "Hasta Bakım", [4][5]string{
				{"Temel Hasta Bakımı", "Anatomi", "Fizyoloji", "Hasta Bakımında İletişim", "İlk Yardım"},
				{"Hastalıklarda Bakım", "Hasta Güvenliği", "Enfeksiyon Kontrolü", "Beslenme ve Diyet", "Farmakolojiye Giriş"},
				{"Yaşlı Bakımı", "Palyatif Bakım", "Evde Bakım Hizmetleri", "Rehabilitasyon Temelleri", "Sağlık Yönetimi"},
				{"Kronik Hastalık Bakımı", "Yoğun Bakım Temelleri", "Bakım Planlama", "Mesleki Etik", "Klinik Uygulama"}}},
		}},
	{Code: "SPOR", Name: "Spor Bilimleri Fakültesi", LabRate: 0.05, LecturerCount: 5,
		Depts: []deptDef{
			{"BES", "Beden Eğitimi ve Spor Öğretmenliği", [4][5]string{
				{"Spor Anatomisi", "Genel Fizyoloji", "Eğitim Bilimine Giriş", "Atletizm", "Jimnastik"},
				{"Egzersiz Fizyolojisi", "Öğrenme ve Öğretim", "Voleybol", "Basketbol", "Spor Psikolojisi"},
				{"Spor Biyomekaniği", "Beden Eğitimi Öğretim Yöntemleri", "Futbol", "Yüzme", "Ölçme ve Değerlendirme"},
				{"Okullarda Beden Eğitimi Uygulaması", "Spor Sosyolojisi", "Özel Eğitimde Spor", "Hentbol", "Öğretmenlik Uygulaması"}}},
			{"ANT", "Antrenörlük Eğitimi", [4][5]string{
				{"Antrenman Bilgisi I", "Spor Anatomisi", "Spor Fizyolojisi", "Atletizm", "Spor Tarihi"},
				{"Antrenman Bilgisi II", "Hareket Bilgisi", "Spor Beslenmesi", "Takım Sporları Antrenmanı", "Spor Psikolojisi"},
				{"Performans Analizi", "Müsabaka ve Taktik", "Spor Sakatlıkları", "Güç ve Kondisyon", "Genç Sporcu Gelişimi"},
				{"Elit Sporcu Antrenmanı", "Antrenörlükte Yönetim", "Spor Bilimlerinde Araştırma", "Spor Hukuku", "Uygulamalı Antrenörlük"}}},
			{"SPY", "Spor Yöneticiliği", [4][5]string{
				{"Spor Yönetimine Giriş", "İşletme Temelleri", "Spor Sosyolojisi", "Genel Muhasebe", "Spor Tarihi"},
				{"Spor Pazarlaması", "Spor Organizasyonları", "Spor Ekonomisi", "Halkla İlişkiler", "İstatistik"},
				{"Spor Tesisleri Yönetimi", "Spor Hukuku", "Spor Turizmi", "Finansal Yönetim", "Etkinlik Yönetimi"},
				{"Spor Kulüplerinde Yönetim", "Sponsorluk ve Medya", "Stratejik Yönetim", "Spor Politikaları", "Bitirme Projesi"}}},
		}},
}

// allDepartments, fakülte sırasıyla tüm bölümleri (kimlik = sıra+1) ve bağlı fakülte indeksini verir.
func allDepartments() (defs []deptDef, facultyOf []int) {
	for fi, f := range faculties {
		for _, d := range f.Depts {
			defs = append(defs, d)
			facultyOf = append(facultyOf, fi)
		}
	}
	return
}
