// agerange: random-access decryption of age v1 files (spec 10.2). typage
// unwraps the file key from the header; agerange decrypts any plaintext range
// from it, reading only the chunks that hold the range.
export { AgeRangeError, headerLength } from './header';
export { openAgeRange, type AgeRange, type RangeSource } from './agerange';
