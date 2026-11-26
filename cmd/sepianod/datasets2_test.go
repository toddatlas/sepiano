package main

import "testing"

func TestBatch2Names(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{personName("Carreño, Teresa (1853-1917)"), "Teresa Carreño"},
		{personName("Albert, Eugen d' (1864-1932)"), "Eugen d'Albert"},
		{personName("Del Grande, Carlo (1867-); Utz, Paula"), "Carlo Del Grande & Paula Utz"},
		{personName("Grünfeld, Alfred"), "Alfred Grünfeld"},
		{surname("Chopin, Frédéric (1810-1849)"), "Chopin"},
		{surname("Mendelssohn-Bartholdy, Felix (1809-1847)"), "Mendelssohn"},
		{surname(""), ""},
		{supraTitle("Polonaise : (polonaise militaire) : op. 40, no. 1"), "Polonaise (polonaise militaire), op. 40, no. 1"},
		{asapTitle("Beethoven", "Piano_Sonatas_31-3_4"), "Piano Sonata No. 31: III–IV"},
		{asapTitle("Schubert", "Piano_Sonatas_664-2"), "Piano Sonata, D. 664: II"},
		{asapTitle("Chopin", "Sonata_2_3rd"), "Piano Sonata No. 2: III"},
		{asapTitle("Bach", "Fugue_bwv_865"), "Fugue, BWV 865"},
		{asapTitle("Schubert", "Impromptu_op.90_D.899_4"), "Impromptu, Op. 90 No. 4 (D. 899)"},
		{asapTitle("Ravel", "Pavane"), "Pavane pour une infante défunte"},
		{asapPlayer("Rizikov01M.mid"), "Rizikov"},
		{asapPlayer("ZhangE01M.mid"), "Zhang E."},
		{asapPlayer("CHEN04.mid"), "Chen"},
		{asapPlayer("KIM_J01.mid"), "Kim J."},
		{asapPlayer("Feiner_2006_01.mid"), "Feiner"},
		{asapPlayer("mikahailoff02.mid"), "Mikahailoff"},
		{asapPlayer("01.mid"), "Piano-e-Competition"},
		{oneSpace(" Solo Piano -  Standards "), "Solo Piano - Standards"},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
	title, venue := pijamaTitle("Begin The Beguine - Live At Maybeck Recital Hall, Berkeley, CA July 19, 1992")
	if title != "Begin The Beguine" || venue != "live at Maybeck Recital Hall, Berkeley, CA July 19, 1992" {
		t.Errorf("pijamaTitle: %q, %q", title, venue)
	}
	if title, venue := pijamaTitle("Yesterdays - Live"); title != "Yesterdays" || venue != "live" {
		t.Errorf("pijamaTitle: %q, %q", title, venue)
	}
	if title, venue := pijamaTitle("Moon Ra"); title != "Moon Ra" || venue != "" {
		t.Errorf("pijamaTitle: %q, %q", title, venue)
	}
}
