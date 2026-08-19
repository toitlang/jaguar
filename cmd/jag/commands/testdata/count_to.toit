// A self-contained debug target. count-to 5 == 0+1+2+3+4 == 10.
main:
  result := count-to 5
  print "result=$result"

count-to n/int -> int:
  sum := 0
  for i := 0; i < n; i++:
    sum += i
  return sum
