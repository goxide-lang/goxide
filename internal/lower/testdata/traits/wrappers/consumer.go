package main

import (
	"bytes"
	"fmt"
	"io"
	"reflect"

	"traitwrappers/adapters"
	"traitwrappers/other"
)

func main() {
	buffer := bytes.NewBufferString("source")
	wrapped := adapters.Wrap(buffer)
	reader := adapters.AsBufferReader(&wrapped)
	_, sourceWriterTo := any(buffer).(io.WriterTo)
	_, viewWriterTo := any(reader).(io.WriterTo)
	_, sourceTrait := any(buffer).(adapters.Reader)
	fmt.Println(sourceWriterTo, viewWriterTo, sourceTrait, buffer.Len())
	var output bytes.Buffer
	n, err := io.Copy(&output, reader)
	fmt.Println(n, output.String(), err, buffer.Len())
	buffer.WriteString("pair")
	ptrReader := adapters.AsPointerBufferPtrReader(adapters.WrapPointer(buffer))
	chunk := make([]byte, 2)
	nRead, readErr := ptrReader.Read(chunk)
	fmt.Println(nRead, string(chunk), readErr, buffer.Len())
	nRead, readErr = adapters.CopyRead(&wrapped, chunk)
	fmt.Println(nRead, string(chunk), readErr, buffer.Len())
	otherBuffer := bytes.NewBufferString("other")
	otherWrapped := other.Wrap(otherBuffer)
	otherReader := other.AsBufferReader(&otherWrapped)
	data, err := io.ReadAll(otherReader)
	fmt.Println(string(data), err)
	otherBuffer.WriteString("again")
	data, err = io.ReadAll(otherWrapped.AsReader())
	fmt.Println(string(data), err)
	otherBuffer.WriteString("view")
	otherView := other.View(&otherWrapped)
	data, err = io.ReadAll(otherView)
	fmt.Println(string(data), err, reflect.TypeOf(otherView) == reflect.TypeOf(otherReader), reflect.TypeOf(otherReader) != reflect.TypeOf(reader))
}
