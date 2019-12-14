// +build !windows

package hx711

import (
	"fmt"
	"log"
	"sort"
	"time"

	"periph.io/x/periph/conn/gpio"
	"periph.io/x/periph/conn/gpio/gpioreg"
	"periph.io/x/periph/host"
)

// HostInit calls periph.io host.Init(). This needs to be done before Hx711 can be used.
func HostInit() error {
	_, err := host.Init()
	return err
}

// NewHx711 creates new Hx711.
// Make sure to set clockPinName and dataPinName to the correct pins.
// https://cdn.sparkfun.com/datasheets/Sensors/ForceFlex/hx711_english.pdf
func NewHx711(clockPinName string, dataPinName string) (*Hx711, error) {
	hx711 := &Hx711{numEndPulses: 1}

	hx711.clockPin = gpioreg.ByName(clockPinName)
	if hx711.clockPin == nil {
		return nil, fmt.Errorf("clockPin is nill")
	}
	err := hx711.clockPin.Out(gpio.Low)
	if err != nil {
		return nil, fmt.Errorf("Error setting clockPin to low %w", err)
	}

	hx711.dataPin = gpioreg.ByName(dataPinName)
	if hx711.dataPin == nil {
		return nil, fmt.Errorf("dataPin is nill")
	}

	err = hx711.dataPin.In(gpio.PullNoChange, gpio.FallingEdge)
	if err != nil {
		return nil, fmt.Errorf("dataPin setting to in error: %w", err)
	}

	err = hx711.applyGain()
	if err != nil {
		return nil, fmt.Errorf("error applying gain %w", err)
	}
	return hx711, nil
}

// SetGain can be set to gain of 128, 64, or 32.
// Gain of 128 or 64 is input channel A, gain of 32 is input channel B.
// Default gain is 128.
// Note change only takes affect after one reading.
func (hx711 *Hx711) SetGain(gain int) error {
	switch gain {
	case 128:
		hx711.numEndPulses = 1
	case 64:
		hx711.numEndPulses = 3
	case 32:
		hx711.numEndPulses = 2
	default:
		hx711.numEndPulses = 1
	}
	err := hx711.applyGain()
	if err != nil {
		return fmt.Errorf("Error while reading data after gain change to %d: %w", gain, err)
	}
	return nil
}

func (hx711 *Hx711) applyGain() error {
	var err error
	for i := 0; i < 5; i++ {
		// read data to trigger channel change
		_, err = hx711.ReadDataRaw()
		if err == nil {
			// sleep for at least 400ms for settling time after channel change
			// sleep is actually done in waitForDataReady, no need to sleep here
			// time.Sleep(410 * time.Millisecond)
			return nil
		}
	}
	return fmt.Errorf("Error while reading data after 5 tries while applying gain %w", err)
}

// setClockHighThenLow sets clock pin high then low
func (hx711 *Hx711) setClockHighThenLow() error {
	startTime := time.Now()
	err := hx711.clockPin.Out(gpio.High)
	if err != nil {
		return fmt.Errorf("set clock pin to high error: %w", err)
	}
	err = hx711.clockPin.Out(gpio.Low)
	if err != nil {
		return fmt.Errorf("set clock pin to low error: %w", err)
	}
	// add margin to account for calls around high/low ?
	if d := time.Now().Sub(startTime); d >= 60*time.Microsecond {
		defer hx711.applyGain()
		return fmt.Errorf("clock was high for too long: %v", d)
	}
	return nil
}

// Reset starts up or resets the chip.
// The chip needs to be reset if it is not used for just about any amount of time.
func (hx711 *Hx711) Reset() error {
	err := hx711.clockPin.Out(gpio.Low)
	if err != nil {
		return fmt.Errorf("set clock pin to low error: %w", err)
	}
	err = hx711.clockPin.Out(gpio.High)
	if err != nil {
		return fmt.Errorf("set clock pin to high error: %w", err)
	}
	time.Sleep(70 * time.Microsecond)
	err = hx711.clockPin.Out(gpio.Low)
	if err != nil {
		return fmt.Errorf("set clock pin to low error: %w", err)
	}
	err = hx711.applyGain()
	if err != nil {
		return fmt.Errorf("Error while apply gain after reset %w", err)
	}
	return nil
}

// Shutdown puts the chip in powered down mode.
// The chip should be shutdown if it is not used for just about any amount of time.
func (hx711 *Hx711) Shutdown() error {
	err := hx711.clockPin.Out(gpio.High)
	if err != nil {
		return fmt.Errorf("set clock pin to high error: %w", err)
	}
	return nil
}

// waitForDataReady waits for data to go to low which means chip is ready
func (hx711 *Hx711) waitForDataReady() error {
	err := hx711.clockPin.Out(gpio.Low)
	if err != nil {
		return fmt.Errorf("set clock pin to low error: %w", err)
	}

	var level gpio.Level

	// looks like chip often takes 80 to 100 milliseconds to get ready
	// but somettimes it takes around 500 milliseconds to get ready
	// WaitForEdge sometimes returns right away
	// So will loop for 11, which could be more than 1 second, but usually 500 milliseconds
	for i := 0; i < 11; i++ {
		level = hx711.dataPin.Read()
		if level == gpio.Low {
			return nil
		}
		hx711.dataPin.WaitForEdge(100 * time.Millisecond)
	}

	return fmt.Errorf("timeout")
}

// ReadDataRaw will get one raw reading from chip.
// Usually will need to call Reset before calling this and Shutdown after.
func (hx711 *Hx711) ReadDataRaw() (int, error) {
	err := hx711.waitForDataReady()
	if err != nil {
		return 0, fmt.Errorf("waitForDataReady error: %w", err)
	}

	var level gpio.Level
	var data int
	for i := 0; i < 24; i++ {
		// respect minimal interval of 0.2us, typ. of 1us between rise
		err = hx711.setClockHighThenLow()
		if err != nil {
			return 0, fmt.Errorf("setClockHighThenLow error: %w", err)
		}
		// max raise time is 0.1us after clock high, so wait at least for that long before read

		level = hx711.dataPin.Read()
		data = data << 1
		if level == gpio.High {
			data++
		}
	}

	for i := 0; i < hx711.numEndPulses; i++ {
		err = hx711.setClockHighThenLow()
		if err != nil {
			return 0, fmt.Errorf("setClockHighThenLow error: %w", err)
		}
	}

	// if high 24 bit is set, value is negtive
	// 100000000000000000000000
	if (data & 0x800000) > 0 {
		// flip bits 24 and lower to get negtive number for int
		// 111111111111111111111111
		data |= ^0xffffff
	}

	return data, nil
}

// readDataMedianRaw will get median of numReadings raw readings.
func (hx711 *Hx711) readDataMedianRaw(numReadings int, stop *bool) (int, error) {
	var err error
	var data int
	datas := make([]int, 0, numReadings)

	for i := 0; i < numReadings; i++ {
		if *stop {
			return 0, fmt.Errorf("stopped")
		}

		data, err = hx711.ReadDataRaw()
		if err != nil {
			continue
		}
		// reading of -1 seems to be some kind of error
		if data == -1 {
			continue
		}
		datas = append(datas, data)
	}

	if len(datas) < 1 {
		return 0, fmt.Errorf("no data, last err: %w", err)
	}

	sort.Ints(datas)

	return datas[len(datas)/2], nil
}

// ReadDataMedianRaw will get median of numReadings raw readings.
// Do not call Reset before or Shutdown after.
// Reset and Shutdown are called for you.
func (hx711 *Hx711) ReadDataMedianRaw(numReadings int) (int, error) {
	var data int

	// err := hx711.Reset()
	// if err != nil {
	// 	return 0, fmt.Errorf("Reset error: %v", err)
	// }

	stop := false
	data, err := hx711.readDataMedianRaw(numReadings, &stop)

	// hx711.Shutdown()

	return data, err
}

// ReadDataMedian will get median of numReadings raw readings,
// then will adjust number with AdjustZero and AdjustScale.
// Do not call Reset before or Shutdown after.
// Reset and Shutdown are called for you.
func (hx711 *Hx711) ReadDataMedian(numReadings int) (float64, error) {
	data, err := hx711.ReadDataMedianRaw(numReadings)
	if err != nil {
		return 0, err
	}
	return float64(data-hx711.AdjustZero) / hx711.AdjustScale, nil
}

// ReadDataMedianThenAvg will get median of numReadings raw readings,
// then do that numAvgs number of time, and average those.
// then will adjust number with AdjustZero and AdjustScale.
// Do not call Reset before or Shutdown after.
// Reset and Shutdown are called for you.
func (hx711 *Hx711) ReadDataMedianThenAvg(numReadings, numAvgs int) (float64, error) {
	var sum int
	for i := 0; i < numAvgs; i++ {
		data, err := hx711.ReadDataMedianRaw(numReadings)
		if err != nil {
			return 0, err
		}
		sum += data - hx711.AdjustZero
	}
	return (float64(sum) / float64(numAvgs)) / hx711.AdjustScale, nil
}

// ReadDataMedianThenMovingAvgs will get median of numReadings raw readings,
// then will adjust number with AdjustZero and AdjustScale. Stores data into previousReadings.
// Then returns moving average.
// Do not call Reset before or Shutdown after.
// Reset and Shutdown are called for you.
// Will panic if previousReadings is nil
func (hx711 *Hx711) ReadDataMedianThenMovingAvgs(numReadings, numAvgs int, previousReadings *[]float64) (float64, error) {
	data, err := hx711.ReadDataMedian(numReadings)
	if err != nil {
		return 0, err
	}

	if len(*previousReadings) < numAvgs {
		*previousReadings = append(*previousReadings, data)
	} else {
		*previousReadings = append((*previousReadings)[1:numAvgs], data)
	}

	var result float64
	for i := range *previousReadings {
		result += (*previousReadings)[i]
	}
	return result / float64(len(*previousReadings)), nil
}

// BackgroundReadMovingAvgs it meant to be run in the background, run as a Goroutine.
// Will continue to get readings and update movingAvg until stop is set to true.
// After it has been stopped, the stopped chan will be closed.
// Note when scale errors the movingAvg value will not change.
// Do not call Reset before or Shutdown after.
// Reset and Shutdown are called for you.
// Will panic if movingAvg or stop are nil
func (hx711 *Hx711) BackgroundReadMovingAvgs(numReadings, numAvgs int, movingAvg *float64, stop *bool, stopped chan struct{}) {
	var err error
	var data int
	var result float64
	previousReadings := make([]float64, 0, numAvgs)

	for {
		err = hx711.Reset()
		if err == nil {
			break
		}
		log.Print("hx711 BackgroundReadMovingAvgs Reset error:", err)
		time.Sleep(time.Second)
	}

	for !*stop {
		data, err = hx711.readDataMedianRaw(numReadings, stop)
		if err != nil && err.Error() != "stopped" {
			log.Print("hx711 BackgroundReadMovingAvgs ReadDataMedian error:", err)
			continue
		}

		result = float64(data-hx711.AdjustZero) / hx711.AdjustScale
		if len(previousReadings) < numAvgs {
			previousReadings = append(previousReadings, result)
		} else {
			previousReadings = append(previousReadings[1:numAvgs], result)
		}

		result = 0
		for i := range previousReadings {
			result += previousReadings[i]
		}

		*movingAvg = result / float64(len(previousReadings))
	}

	hx711.Shutdown()

	close(stopped)
}

// GetAdjustValues will help get you the adjust values to plug in later.
// Do not call Reset before or Shutdown after.
// Reset and Shutdown are called for you.
func (hx711 *Hx711) GetAdjustValues(weight1 float64, weight2 float64) {
	var err error
	var adjustZero int
	var scale1 int
	var scale2 int

	fmt.Println("Make sure scale is working and empty, getting weight in 5 seconds...")
	time.Sleep(5 * time.Second)
	fmt.Println("Getting weight...")
	adjustZero, err = hx711.ReadDataMedianRaw(11)
	if err != nil {
		fmt.Println("ReadDataMedianRaw error:", err)
		return
	}
	fmt.Println("Raw weight is:", adjustZero)
	fmt.Println("")

	fmt.Printf("Put first weight of %.2f on scale, getting weight in 15 seconds...\n", weight1)
	time.Sleep(15 * time.Second)
	fmt.Println("Getting weight...")
	scale1, err = hx711.ReadDataMedianRaw(11)
	if err != nil {
		fmt.Println("ReadDataMedianRaw error:", err)
		return
	}
	fmt.Println("Raw weight is:", scale1)
	fmt.Println("")

	fmt.Printf("Put second weight of %.2f on scale, getting weight in 15 seconds...\n", weight2)
	time.Sleep(15 * time.Second)
	fmt.Println("Getting weight...")
	scale2, err = hx711.ReadDataMedianRaw(11)
	if err != nil {
		fmt.Println("ReadDataMedianRaw error:", err)
		return
	}
	fmt.Println("Raw weight is ", scale2)
	fmt.Println("")

	adjust1 := float64(scale1-adjustZero) / weight1
	adjust2 := float64(scale2-adjustZero) / weight2

	fmt.Println("AdjustZero should be set to:", adjustZero)
	fmt.Printf("AdjustScale should be set to a value between %f and %f\n", adjust1, adjust2)
	fmt.Println("")
}
