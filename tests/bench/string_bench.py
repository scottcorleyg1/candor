import time

def string_test():
    s = ""
    for i in range(10000):
        s += "A"
    return len(s)

start = time.time()
r = string_test()
end = time.time()
print(f"string_test len: {r}")
print(f"Time: {int((end - start)*1000)}ms")
