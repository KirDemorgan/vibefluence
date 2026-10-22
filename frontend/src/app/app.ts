import { Component } from '@angular/core';
import { RouterOutlet } from '@angular/router';
import { TuiRoot } from '@taiga-ui/core';

@Component({
  selector: 'vf-root',
  imports: [RouterOutlet, TuiRoot],
  templateUrl: './app.html',
})
export class App {}
