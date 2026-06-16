import Workspace from './routes/+page.svelte' 
import Login from './routes/login/+page.svelte'
import Projects from './routes/projects/+page.svelte'

const routes = {
    // Exact path
    '/': Workspace,

    // Using named parameters, with last being optional
    '/login': Login,

    '/Projects': Projects,

}