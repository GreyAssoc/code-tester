def used():
    return 1

def dead_fn():
    return 2

def stub():
    pass

def todo():
    raise NotImplementedError("todo")

def scale(v, factor):
    return v * 2

def main():
    print(used())

def bad_ret() -> int:
    return "nope"

def greet(name: str = 1) -> str:
    return name
