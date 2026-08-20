export async function withCleanup<T>(body: () => Promise<T>, cleanup: () => Promise<unknown>): Promise<T> {
  let result: T;
  try {
    result = await body();
  } catch (bodyError) {
    try {
      await cleanup();
    } catch (cleanupError) {
      if (bodyError instanceof Error) {
        bodyError.cause = cleanupError;
      }
    }
    throw bodyError;
  }
  await cleanup();
  return result;
}
