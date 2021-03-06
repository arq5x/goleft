package covmed

import (
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	arg "github.com/alexflint/go-arg"
	"github.com/biogo/hts/bam"
	"github.com/biogo/hts/sam"
	"github.com/brentp/xopen"
)

var cli = struct {
	N       int    `arg:"-n,help:number of reads to sample for length"`
	Bam     string `arg:"positional,required,help:bam for which to estimate coverage"`
	Regions string `arg:"positional,help:optional bed file to specify target regions"`
}{N: 100000}

func pcheck(e error) {
	if e != nil {
		panic(e)
	}
}

func readCoverage(path string) int {
	fh, err := xopen.Ropen(path)
	pcheck(err)
	cov := 0
	for {
		line, err := fh.ReadString('\n')

		if err == io.EOF {
			break
		}
		line = strings.TrimSuffix(line, "\n")
		toks := strings.SplitN(line, "\t", 5)
		s, err := strconv.Atoi(toks[1])
		pcheck(err)
		e, err := strconv.Atoi(toks[2])
		pcheck(err)
		cov += e - s
	}
	return cov
}

func meanStd(arr []int) (mean, std float64) {
	l := float64(len(arr))
	for _, a := range arr {
		mean += float64(a) / l
	}
	for _, a := range arr {
		std += math.Pow(float64(a)-mean, 2) / l
	}
	return mean, math.Sqrt(std)
}

// Sizes hold info about a bam returned from BamInsertSizes
type Sizes struct {
	InsertMean       float64
	InsertSD         float64
	TemplateMean     float64
	TemplateSD       float64
	ReadLengthMean   float64
	ReadLengthMedian float64
}

func (s Sizes) String() string {
	return fmt.Sprintf("%.2f\t%.2f\t%.2f\t%.2f", s.InsertMean, s.InsertSD, s.TemplateMean, s.TemplateSD)
}

// BamInsertSizes takes bam reader sample N well-behaved sites and return the coverage and insert-size info
func BamInsertSizes(br *bam.Reader, n int) Sizes {
	sizes := make([]int, 0, cli.N)
	insertSizes := make([]int, 0, cli.N)
	templateLengths := make([]int, 0, cli.N)
	for len(insertSizes) < n {
		rec, err := br.Read()
		if err == io.EOF {
			break
		}
		pcheck(err)
		if rec.Flags&(sam.Secondary|sam.Supplementary|sam.Unmapped|sam.QCFail) != 0 {
			continue
		}
		if len(sizes) < n {
			_, read := rec.Cigar.Lengths()
			sizes = append(sizes, read)
		}

		if rec.Pos < rec.MatePos && rec.Flags&sam.ProperPair == sam.ProperPair && len(rec.Cigar) == 1 && rec.Cigar[0].Type() == sam.CigarMatch {
			insertSizes = append(insertSizes, rec.MatePos-rec.End())
			templateLengths = append(templateLengths, rec.TempLen)
		}

	}

	sort.Ints(sizes)

	s := Sizes{}
	s.ReadLengthMedian = float64(sizes[(len(sizes)-1)/2]) - 1
	s.ReadLengthMean, _ = meanStd(sizes)

	s.InsertMean, s.InsertSD = meanStd(insertSizes)
	s.TemplateMean, s.TemplateSD = meanStd(templateLengths)
	return s
}

// Main is called from the dispatcher
func Main() {

	arg.MustParse(&cli)
	log.Println(cli.Bam)

	fh, err := os.Open(cli.Bam)
	pcheck(err)

	brdr, err := bam.NewReader(fh, 2)
	pcheck(err)

	ifh, ierr := os.Open(cli.Bam + ".bai")
	if ierr != nil {
		// if .bam.bai didn't exist, check .bai
		ifh, err = os.Open(cli.Bam[:len(cli.Bam)-4] + ".bai")
	}
	pcheck(err)

	idx, err := bam.ReadIndex(ifh)
	pcheck(err)

	genomeBases := 0
	mapped := uint64(0)
	for _, ref := range brdr.Header().Refs() {
		stats, ok := idx.ReferenceStats(ref.ID())
		if !ok {
			fmt.Fprintf(os.Stderr, "chromosome: %s not found in %s\n", ref.Name(), cli.Bam)
			continue
		}
		genomeBases += ref.Len()
		mapped += stats.Mapped

	}
	if cli.Regions != "" {
		genomeBases = readCoverage(cli.Regions)
	}

	// TODO: check that reads are from coverage regions.
	sizes := BamInsertSizes(brdr, cli.N)
	coverage := float64(mapped) * sizes.ReadLengthMedian / float64(genomeBases)

	fmt.Fprintf(os.Stdout, "%.2f\t%s\n", coverage, sizes.String())
}
