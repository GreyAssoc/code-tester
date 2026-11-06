export function used(x: number): number {
  return x + 1;
}

export function deadExport(y: number): number {
  return y;
}

export function stub(): void {}

export function broken(a: string): void {
  throw new Error("not implemented");
}

export function scale(v: number, factor: number): number {
  return v * 2;
}

console.log(used(1));

export function badRet(): number {
  return "nope";
}

export function greet(name: string = 1): string {
  return name;
}
