# Benzer sistemler ve geliştirme planı

Amaç: SınavPlan'ı benzer sistemlerle karşılaştırıp bir sonraki adımlarda neyin değer katacağını sıralamak.
**Not:** Bu belge dört web aramasına dayanır; ticari ürünlerin ayrıntılı belgelerine erişilemedi. Bulgular eğilim gösterir, kapsamlı bir pazar analizi değildir. Teslim tarihi (26 Ekim 2026 23:59) sonrası commit'ler değerlendirmeye alınmadığından, aşağıdaki işler teslimden **sonra** ya da mülakat anlatısı olarak düşünülmüştür.

## 1. İncelenen benzer sistemler ve çıkarımlar

| Kaynak | Ne yapıyor | SınavPlan'da karşılığı |
|---|---|---|
| [UniTime — Examination Timetabling](https://help.unitime.org/manuals/examination-timetabling) | Çakışma, ardışık sınav ve günde 2'den fazla sınavı en aza indirir; salon için "sınav oturma kapasitesi" ile normal kapasite ayrı; salona dönem bazlı yasak/kaçınma; erken/geç dönem rotasyon cezası | Var: çakışma, günde ≤2, ardışık gün cezası, etkin kapasite. **Yok:** salon–dönem uygunluğu (belirli oturumda kapalı salon), rotasyon cezası |
| [ITC 2007 Sınav Hattı](https://www.eeecs.qub.ac.uk/itc2007/examtrack/exam_track_index_files/examevaluation.htm) ([OptaPlanner/Timefold özeti](https://optaplanner.org/docs/optaplanner/latest/use-cases-and-examples/exam-timetabling/exam-timetabling.html)) | Akademik kıyas veri kümeleri; sert: öğrenci çakışması, salon kapasitesi, dönem süresi, birliktelik/dışlama/sıralama; salona yalnız bir sınav (exclusive); yumuşak: ardışık, aynı gün 2 sınav, dönem yayılımı, karışık süre, büyük sınavı öne alma | Var: çakışma, kapasite, ardışık/aynı gün cezası. **Yok:** birliktelik/dışlama/sıralama kısıtı, "salonu yalnız bu sınav kullansın", karışık süreli sınavlar, büyük sınavı öne alma, standart kıyas girdisi |
| [ExIMS (UiTM) ve sınav ofisi nöbet çizelgesi araçları](https://www.theexamsoffice.org/?p=26785) | Gözetmen müsaitlik beyanı, görevi kabul/ret, görev takası (çakışma denetimiyle), aynı kıdemde takas politikası, ≥24 saat önceden bildirim | Var: hasta gözetmeni minimum değişiklikle çıkarma. **Yok:** gözetmenin kendi müsaitliği/tercihi, görevi kabul-ret, gözetmenler arası takas, kıdem/unvan kuralı |
| [Qwickly (Chapman)](https://blogs.chapman.edu/academics/2024/08/12/new-features-available-in-qwickly-attendance/), [Sebha prototipi](https://sebhau.edu.ly/journal/index.php/sucp/article/view/4184), [UiTM QR yoklama](https://ir.uitm.edu.my/id/eprint/21334/) | Birkaç saniyede yenilenen QR, koltuk krokisinden yoklama, mükerrer giriş tespiti, devamsızlık uyarısı, sınav takvimine bağlı çalışma, gelmeyen öğrenciye bildirim önerisi | Var: 30 sn'de yenilenen QR + 6 haneli kod, mükerrer tespiti, kroki + liste. **Yok:** gelmeyene otomatik bildirim, krokide koltuğa tıklayıp işaretleme |
| Türkçe/uluslararası gözetmen atama çalışmaları ([Hacettepe](https://research.hacettepe.edu.tr/en/publications/invigilators-assignment-in-practical-examination-timetabling-prob-3/)) | İş yükü adaleti, ardışık görev azaltma, uzmanlık/meslek dikkate alma | Var: adalet (fark ≤1–2), ardışık yok. **Yok:** uzmanlık/unvan, tercih |

**Sonuç:** Çekirdek çizelgeleme kalitemiz akademik literatürle aynı düzeyde. Eksik olan: (a) **gözetmen tarafı self-servis** (müsaitlik, kabul-ret, takas), (b) **bildirim altyapısı**, (c) **ek kısıt türleri** (ITC/UniTime), (d) **standart kıyas** ve (e) sınav günü **özel durumlar**.

## 2. Plan (öncelik ve kabul ölçütü)

Çaba: S ≈ yarım gün, M ≈ 1–2 gün, L ≈ 3+ gün.

### P1 — Değer/çaba en yüksek
| # | İş | Çaba | Kabul ölçütü |
|---|---|---|---|
| 1 | **Gözetmen self-servis**: müsaitlik girişi, görevi kabul/ret, iki gözetmen arası takas isteği (sekreter onaylı) | M | Takas sonrası `T-4` ihlali 0 (bağımsız doğrulayıcı); onaylı mazeretli gözetmene görev atanmaz; takas `dry_run` önizlemeli |
| 2 | **Bildirim kutusu**: görev atandı/değişti, mazeret kararı, salon değişikliği, sınav 24 saat kala hatırlatma (uygulama içi; e-posta SMTP ile isteğe bağlı) | M | Her olay tek bir bildirim üretir (idempotent); okundu bilgisi; kapalıyken sistem çalışır |
| 3 | **Krokiden yoklama**: gözetmen oturma planında koltuğa tıklayıp geldi/gelmedi işaretler (çevrimdışı kuyruğa yazar) | S | Yazılan olay mevcut HLC/çakışma çözümünden geçer; `T-5` testleri geçer |
| 4 | **Gelmeyene bildirim** (sınav başladıktan 15 dk sonra işaretsiz öğrenciye) | S | #2 üzerine; yalnız yayındaki çizelge; mazeretli öğrenciye gitmez |

### P2 — Çizelgeleme zenginleştirme
| # | İş | Çaba | Kabul ölçütü |
|---|---|---|---|
| 5 | **Ek kısıt türleri**: birliktelik (aynı oturum), dışlama, sıralama (A, B'den önce), salon özel kullanım | M | Her kısıt için bağımsız doğrulayıcı + bozulmuş plan testi (Kanıtlar ekranı kalıbı) |
| 6 | **Salon–oturum uygunluğu** (bakım/başka etkinlik nedeniyle kapalı) ve büyük sınavı öne alma cezası | S–M | T-2 doğrulayıcısı kapalı salonda 0 kullanım; ceza metrikleri raporda |
| 7 | **ITC-2007 içe aktarıcı**: resmî örnek dosyayı okuyup aynı çözücüyle çözmek, "uygunluğa uzaklık" ve ceza raporu | L | En az bir örnek için geçerli çözüm + yayımlanmış en iyi sonuçla yan yana tablo (dürüstçe: farkı yazar) |
| 8 | **Ağırlık ayarı ekranı**: ardışık/aynı gün/yayılım ceza ağırlıkları, ön izleme (`dry_run`) ile karşılaştırma | M | Ağırlık değişince metrik farkı gösterilir; varsayılanlar mevcut sonucu bozmaz (altın hash testi) |

### P3 — Sınav günü ve kurumsal
| # | İş | Çaba | Kabul ölçütü |
|---|---|---|---|
| 9 | **Ek süre / ayrı salon** gereken öğrenciler (engelli dışında) ve olay/tutanak kaydı (kopya, sağlık, yangın) | M | Tutanak eklemeli-yalnız denetim tablosuna yazılır |
| 10 | **KVKK**: kişisel veri envanteri, saklama süresi, denetim kaydı dışa aktarma, öğrenci verisi maskeleme | M | Belge + silme/anonimleştirme komutu + test |
| 11 | **ÖBS/SIS entegrasyonu için API kontratı** (CSV yerine imzalı webhook/pull) | L | Sahte ÖBS ile uçtan uca test; gerçek ÖBS erişimi olmadığı belgede açık yazılır |
| 12 | **İzleme**: Prometheus + hazır Grafana panosu, yedekleme/geri yükleme betiği | S–M | Pano repoda; geri yükleme sonrası `make verify` geçer |

## 3. Önerilen sıra
1. **Teslimden önce yeni özellik eklemeyin** (bugün 26 Ekim; kalan süre doğrulama soruları, repoyu public yapma ve anket bağlantısı için). Mevcut sürüm CI'da yeşil.
2. Teslimden sonra mülakat öncesi: #1 → #2 → #3 (gözetmen/bildirim tarafı en görünür fark).
3. Ardından #5–#6 (kısıt zenginleştirme) ve #7 (kıyas) — "çözücüm literatürle nasıl kıyaslanır" sorusuna somut cevap verir.

## 4. Riskler
- Bildirim (e-posta/SMS) gerçek bir sağlayıcı gerektirir; test edilemeyen bir "gönderildi" iddiası yapılmamalı. Önce uygulama içi kutu, e-posta ayrı ve isteğe bağlı.
- ITC-2007 kıyası, çözücünün zaman bütçesi ve kısıt kümesi farklı olduğundan birebir karşılaştırma değildir; bu belirtilerek sunulmalı.
- Gözetmen takası "minimum değişiklik" ilkesini ve adalet farkını bozabilir; her takas bağımsız doğrulayıcıdan geçmeden uygulanmamalı.
