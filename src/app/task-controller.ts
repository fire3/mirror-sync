export class TaskController {
  private state: 'running' | 'paused' | 'cancelled' = 'running';
  private resolvePause: (() => void) | undefined = undefined;

  pause() {
    if (this.state === 'running') {
      this.state = 'paused';
    }
  }

  resume() {
    if (this.state === 'paused') {
      this.state = 'running';
      if (this.resolvePause) {
        this.resolvePause();
        this.resolvePause = undefined;
      }
    }
  }

  cancel() {
    this.state = 'cancelled';
    if (this.resolvePause) {
      this.resolvePause();
      this.resolvePause = undefined;
    }
  }

  isPaused() {
    return this.state === 'paused';
  }

  isCancelled() {
    return this.state === 'cancelled';
  }

  getState() {
    return this.state;
  }

  async check() {
    if (this.state === 'cancelled') {
      throw new Error('Task cancelled');
    }
    if (this.state === 'paused') {
      await new Promise<void>((resolve) => {
        this.resolvePause = resolve;
      });
      if (this.isCancelled()) {
        throw new Error('Task cancelled');
      }
    }
  }
}
