package probe
func nativeValue()func()int{return func()int{return 7}}
func nativePointer()*func()int{f:=nativeValue();return &f}
