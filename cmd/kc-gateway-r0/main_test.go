package main
import "testing"
func TestR0NumericLoopbackOnly(t *testing.T) {
    for _,addr:=range []string{"127.0.0.1:18090","[::1]:18090"} {
        if err:=requireLoopback(addr);err!=nil{t.Fatalf("%s should be local: %v",addr,err)}
    }
    for _,addr:=range []string{"0.0.0.0:18090","[::]:18090","localhost:18090",
        "198.51.100.3:18090","example.com:18090","127.0.0.1"} {
        if err:=requireLoopback(addr);err==nil{t.Fatalf("%s must be rejected",addr)}
    }
}
